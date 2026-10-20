package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ai-informations/internal/service/llmqueue"
)

// Provider translates a batch of short texts into targetLang ("zh" or "en").
//
// The interface is deliberately tiny and language-pair agnostic so the backend
// can be swapped without touching the pipeline: a cloud chat model (DeepSeek,
// Qwen, Moonshot, OpenAI), a self-hosted OpenAI-compatible server (vLLM,
// LM Studio, Ollama's /v1 endpoint) or a local model wrapper all satisfy it.
type Provider interface {
	// Name identifies the provider in logs and in the translation cache.
	Name() string
	// Model identifies the exact model, so a model upgrade does not silently
	// reuse translations produced by an older one.
	Model() string
	// Translate returns one translation per input text, in the same order.
	Translate(ctx context.Context, texts []string, targetLang string) ([]string, error)
}

// targetLanguageName renders a language code as the name handed to the model.
func targetLanguageName(code string) string {
	switch code {
	case "zh":
		return "Simplified Chinese"
	case "en":
		return "English"
	default:
		return code
	}
}

// systemPrompt is intentionally strict: the model must return a bare JSON array
// so the result can be parsed without heuristics, and it must preserve the
// factual tokens a news headline depends on.
func systemPrompt(targetLang string) string {
	return fmt.Sprintf(`You are a professional news headline and abstract translator.
Translate every input string into %s.
Rules:
1. Preserve all numbers, dates, percentages, ticker symbols, person names, organisation names and place names exactly.
2. Keep the register of a news wire service: factual, compact, no added commentary.
3. Never merge, split, add or drop items.
4. Reply with ONLY a JSON array of strings, in the same order as the input, with no markdown fence and no explanation.
Example reply for two inputs: ["first translation","second translation"]`, targetLanguageName(targetLang))
}

// OpenAICompatProvider talks to any server exposing the OpenAI
// /chat/completions contract. DeepSeek, Qwen (DashScope compatible mode),
// Moonshot, OpenAI, vLLM, LM Studio and Ollama all qualify, which is why this
// one implementation covers every realistic deployment for this app.
type OpenAICompatProvider struct {
	BaseURL string
	APIKey  string
	ModelID string
	Client  *http.Client
	// Local marks an endpoint that serves one request at a time (a model
	// running on this machine). Requests are then serialized through a shared
	// gate so classification, translation and startup healing take turns
	// instead of piling concurrent calls onto the model.
	Local bool
}

func (p *OpenAICompatProvider) Name() string  { return "openai-compatible" }
func (p *OpenAICompatProvider) Model() string { return p.ModelID }

// localGate was a package-level capacity-1 channel; the shared budget now
// lives in llmqueue as the dynamic gate sized by llm_max_concurrency, so one
// limit covers translation, classification and connection probes alike.
func acquireLocal(ctx context.Context) (func(), error) {
	return llmqueue.AcquireGate(ctx)
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
	// MaxTokens bounds Chat replies. Without it llama.cpp applies its own
	// default and cut category arrays mid-JSON ("...\"大宗商品 (Commodities"
	// with no closing bracket), which made every long classification reply
	// unparseable. 1024 comfortably fits the full 40-category list.
	MaxTokens int `json:"max_tokens,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// Translate sends all texts in a single request. Batching matters: translating
// ten headlines one request at a time costs ten round trips and ten times the
// latency, while the model keeps the array order reliably.
//
// If the batch response cannot be parsed (e.g. the model returned numbers
// instead of strings), we fall back to individual translations so that one
// misbehaving entry does not poison the whole batch.
func (p *OpenAICompatProvider) Translate(ctx context.Context, texts []string, targetLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		return nil, fmt.Errorf("translation base URL is empty")
	}
	if strings.TrimSpace(p.ModelID) == "" {
		return nil, fmt.Errorf("translation model is empty")
	}

	// ---- Fast path: try batch translation -----------------------------------
	batch, err := p.translateBatch(ctx, texts, targetLang)
	if err == nil && len(batch) == len(texts) {
		return batch, nil
	}
	if err != nil {
		log.Printf("translator: batch translate failed (%v), falling back to per-item", err)
	}

	// ---- Slow path: one request per text ------------------------------------
	results := make([]string, len(texts))
	var firstErr error
	for i, text := range texts {
		one, err := p.translateOne(ctx, text, targetLang)
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("translation failed for item %d (%q): %w", i, truncate(text, 40), err)
			}
			results[i] = "" // leave placeholder; caller decides what to do
			continue
		}
		results[i] = one
	}
	return results, firstErr
}

func (p *OpenAICompatProvider) translateBatch(ctx context.Context, texts []string, targetLang string) ([]string, error) {
	payload, err := json.Marshal(texts)
	if err != nil {
		return nil, err
	}

	body, err := json.Marshal(chatRequest{
		Model: p.ModelID,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt(targetLang)},
			{Role: "user", Content: string(payload)},
		},
		Temperature: 0,
		Stream:      false,
	})
	if err != nil {
		return nil, err
	}

	return p.doRequest(ctx, body)
}

func (p *OpenAICompatProvider) translateOne(ctx context.Context, text string, targetLang string) (string, error) {
	payload, err := json.Marshal([]string{text})
	if err != nil {
		return "", err
	}

	body, err := json.Marshal(chatRequest{
		Model: p.ModelID,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt(targetLang)},
			{Role: "user", Content: string(payload)},
		},
		Temperature: 0,
		Stream:      false,
	})
	if err != nil {
		return "", err
	}

	results, err := p.doRequest(ctx, body)
	if err != nil {
		return "", err
	}
	if len(results) == 0 {
		return "", fmt.Errorf("provider returned no translation")
	}
	return results[0], nil
}

func (p *OpenAICompatProvider) doRequest(ctx context.Context, body []byte) ([]string, error) {
	if p.Local {
		release, err := acquireLocal(ctx)
		if err != nil {
			return nil, err
		}
		defer release()
	}
	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("translation request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading translation response failed: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("translation response was not JSON (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return nil, fmt.Errorf("translation provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("translation provider returned HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	if len(parsed.Choices) == 0 {
		return nil, fmt.Errorf("translation provider returned no choices: %s", snippet(raw))
	}

	return parseStringArray(parsed.Choices[0].Message.Content)
}

// Chat sends a single-turn conversation and returns the raw assistant message.
// It is the general-purpose entry point for non-translation tasks such as
// article classification.
func (p *OpenAICompatProvider) Chat(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if strings.TrimSpace(p.BaseURL) == "" {
		return "", fmt.Errorf("translation base URL is empty")
	}
	if strings.TrimSpace(p.ModelID) == "" {
		return "", fmt.Errorf("translation model is empty")
	}
	if p.Local {
		release, err := acquireLocal(ctx)
		if err != nil {
			return "", err
		}
		defer release()
	}

	body, err := json.Marshal(chatRequest{
		Model: p.ModelID,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userPrompt},
		},
		Temperature: 0,
		Stream:      false,
		MaxTokens:   1024,
	})
	if err != nil {
		return "", err
	}

	endpoint := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("chat request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("reading chat response failed: %w", err)
	}

	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("chat response was not JSON (HTTP %d): %s", resp.StatusCode, snippet(raw))
	}
	if parsed.Error != nil && parsed.Error.Message != "" {
		return "", fmt.Errorf("chat provider error: %s", parsed.Error.Message)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("chat provider returned HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	if len(parsed.Choices) == 0 {
		return "", fmt.Errorf("chat provider returned no choices: %s", snippet(raw))
	}
	return parsed.Choices[0].Message.Content, nil
}

// markdownFence matches ```json ... ``` or ``` ... ``` blocks.
var markdownFence = regexp.MustCompile("(?s)```\\w*\\n?(.*?)```")

// parseStringArray extracts a JSON array of strings from model output, coping
// with the markdown fence and prose that models add despite instructions.
func parseStringArray(content string) ([]string, error) {
	s := strings.TrimSpace(content)
	if s == "" {
		return nil, fmt.Errorf("translation provider returned empty content")
	}

	// Strip markdown code fences: ```json\n[...]```  →  [...]
	if m := markdownFence.FindStringSubmatch(s); m != nil {
		if inner := strings.TrimSpace(m[1]); inner != "" {
			s = inner
		}
	}

	// raw keeps the fence-stripped reply: the last-resort recovery below needs
	// the text the model left outside the array brackets.
	raw := s

	if start := strings.Index(s, "["); start >= 0 {
		if end := strings.LastIndex(s, "]"); end > start {
			s = s[start : end+1]
		}
	}

	var out []string
	if err := json.Unmarshal([]byte(s), &out); err == nil {
		return out, nil
	}

	// Some models answer with an object keyed by index or with a
	// {"translations": [...]} wrapper; accept those two shapes too.
	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal([]byte(s), &wrapper); err == nil {
		for _, key := range []string{"translations", "result", "results", "data"} {
			if raw, ok := wrapper[key]; ok {
				var arr []string
				if err := json.Unmarshal(raw, &arr); err == nil {
					return arr, nil
				}
			}
		}
		// Index-keyed object: {"0": "...", "1": "..."}
		ordered := make([]string, 0, len(wrapper))
		for i := 0; ; i++ {
			raw, ok := wrapper[fmt.Sprint(i)]
			if !ok {
				break
			}
			var one string
			if err := json.Unmarshal(raw, &one); err != nil {
				break
			}
			ordered = append(ordered, one)
		}
		if len(ordered) > 0 {
			return ordered, nil
		}
	}

	// Last resort: the model returned a malformed array such as
	// `["[headline]" trailing prose]`. Recover the quoted strings so one
	// misbehaving response fails only its own article, not the whole run.
	if out := tolerantParse(raw); len(out) > 0 {
		log.Printf("translator: recovered %d translation(s) from malformed provider output: %s",
			len(out), snippet([]byte(content)))
		return out, nil
	}

	log.Printf("translator: parseStringArray failed, provider returned: %s", snippet([]byte(content)))
	return nil, fmt.Errorf("could not parse translations from provider output: %s", snippet([]byte(content)))
}

// tolerantParse recovers translations from a malformed provider response.
//
// Models occasionally return a broken array such as
// `["[headline]" trailing prose]`: the first element is quoted and the rest is
// not. We scan for quoted string literals; unquoted text after the last one
// is appended to it, because it is usually the remainder of the same
// translation. Returns nil when nothing usable can be recovered.
func tolerantParse(s string) []string {
	if !strings.Contains(s, "[") {
		return nil
	}
	var out []string
	i, n := 0, len(s)
	for i < n {
		q := strings.IndexByte(s[i:], '"')
		if q < 0 {
			break
		}
		start := i + q
		var b strings.Builder
		escaped, closed := false, false
		j := start + 1
		for ; j < n; j++ {
			c := s[j]
			if escaped {
				switch c {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				case 'r':
					b.WriteByte('\r')
				default:
					b.WriteByte(c)
				}
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == '"' {
				closed = true
				break
			}
			b.WriteByte(c)
		}
		if txt := strings.TrimSpace(b.String()); txt != "" {
			out = append(out, b.String())
		}
		if !closed {
			i = n
			break
		}
		i = j + 1
	}
	if len(out) == 0 {
		return nil
	}
	// The model often leaves the tail of the translation outside the quotes:
	// `["[headline]" trailing prose]`. Attach it to the last element.
	if tail := strings.TrimRight(strings.TrimSpace(s[i:]), " \t\r\n,]"); tail != "" {
		out[len(out)-1] += " " + tail
	}
	return out
}

func snippet(b []byte) string {
	const max = 300
	s := strings.TrimSpace(string(b))
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
