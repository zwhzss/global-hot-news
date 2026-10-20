package translator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// MTranServerProvider and LibreTranslateProvider are the two REST translation
// engines that can replace the LLM for the bilingual pipeline. Both expose a
// plain "translate this text" contract instead of a chat model, so they skip
// the prompt/JSON-array dance entirely and cost nothing to run locally.
//
// Language codes: the pipeline speaks "zh"/"en" (langdetect constants) and the
// engines get their own codes (zh-Hans for MTranServer, zh for LibreTranslate).
// The source language is always the opposite of the target, because the
// pipeline only ever bridges English and Chinese.

// ---- MTranServer (xxnuo/MTranServer) ---------------------------------------

// MTranServerProvider talks to the native JSON API of MTranServer:
// POST /translate (single) and POST /translate/batch (batch), optionally
// authenticated with a Bearer token (MT_API_TOKEN, disabled by default).
type MTranServerProvider struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

func (p *MTranServerProvider) Name() string  { return "mtranserver" }
func (p *MTranServerProvider) Model() string { return "mtranserver" }

// mtLang maps the pipeline's language codes onto MTranServer's codes.
func mtLang(code string) string {
	if code == "zh" {
		return "zh-Hans"
	}
	if code == "" {
		return "auto"
	}
	return code
}

type mtRequest struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Text  string   `json:"text,omitempty"`
	Texts []string `json:"texts,omitempty"`
	HTML  bool     `json:"html"`
}

func (p *MTranServerProvider) Translate(ctx context.Context, texts []string, targetLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("MTranServer URL is empty")
	}
	from, to := mtLang(opposite(targetLang)), mtLang(targetLang)

	if len(texts) > 1 {
		out, err := p.post(ctx, base+"/translate/batch", mtRequest{From: from, To: to, Texts: texts})
		if err == nil && len(out) == len(texts) {
			return out, nil
		}
		if err != nil {
			log.Printf("translator: MTranServer batch failed (%v), falling back to per-item", err)
		}
	}

	results := make([]string, len(texts))
	var firstErr error
	for i, text := range texts {
		one, err := p.post(ctx, base+"/translate", mtRequest{From: from, To: to, Text: text})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("MTranServer failed for item %d (%q): %w", i, truncate(text, 40), err)
			}
			continue
		}
		if len(one) == 0 || one[0] == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("MTranServer returned an empty translation for item %d", i)
			}
			continue
		}
		results[i] = one[0]
	}
	return results, firstErr
}

func (p *MTranServerProvider) post(ctx context.Context, url string, payload mtRequest) ([]string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if strings.TrimSpace(p.Token) != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("MTranServer request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading MTranServer response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("MTranServer returned HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return decodeStringList(raw, "result", "results", "texts", "translations", "translatedText", "text")
}

// ---- LibreTranslate ---------------------------------------------------------

// LibreTranslateProvider talks to the standard LibreTranslate REST API:
// POST /translate with q (string or string array), source, target and an
// optional api_key. Docker image libretranslate/libretranslate serves it on
// port 5000.
type LibreTranslateProvider struct {
	BaseURL string
	APIKey  string
	Client  *http.Client
}

func (p *LibreTranslateProvider) Name() string  { return "libretranslate" }
func (p *LibreTranslateProvider) Model() string { return "libretranslate" }

type ltRequest struct {
	Q      any    `json:"q"`
	Source string `json:"source"`
	Target string `json:"target"`
	Format string `json:"format"`
	APIKey string `json:"api_key,omitempty"`
}

func ltLang(code string) string {
	if code == "" {
		return "auto"
	}
	return code
}

func (p *LibreTranslateProvider) Translate(ctx context.Context, texts []string, targetLang string) ([]string, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	base := strings.TrimRight(strings.TrimSpace(p.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("LibreTranslate URL is empty")
	}
	source, target := ltLang(opposite(targetLang)), ltLang(targetLang)

	if len(texts) > 1 {
		out, err := p.post(ctx, base+"/translate", ltRequest{Q: texts, Source: source, Target: target, Format: "text", APIKey: p.APIKey})
		if err == nil && len(out) == len(texts) {
			return out, nil
		}
		if err != nil {
			log.Printf("translator: LibreTranslate batch failed (%v), falling back to per-item", err)
		}
	}

	results := make([]string, len(texts))
	var firstErr error
	for i, text := range texts {
		one, err := p.post(ctx, base+"/translate", ltRequest{Q: text, Source: source, Target: target, Format: "text", APIKey: p.APIKey})
		if err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("LibreTranslate failed for item %d (%q): %w", i, truncate(text, 40), err)
			}
			continue
		}
		if len(one) == 0 || one[0] == "" {
			if firstErr == nil {
				firstErr = fmt.Errorf("LibreTranslate returned an empty translation for item %d", i)
			}
			continue
		}
		results[i] = one[0]
	}
	return results, firstErr
}

func (p *LibreTranslateProvider) post(ctx context.Context, url string, payload ltRequest) ([]string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("LibreTranslate request failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("reading LibreTranslate response failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("LibreTranslate returned HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	return decodeStringList(raw, "translatedText", "translation", "texts", "text")
}

// ---- shared response decoding ----------------------------------------------

// decodeStringList extracts the translations from an engine response without
// betting on one exact shape: engines return a bare array, a single string, or
// an object wrapping either. Known keys are tried in order; an object without
// any of them surfaces its "error"/"message" field so a config mistake reads
// as a real error instead of "empty translation".
func decodeStringList(raw []byte, keys ...string) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty response from translation engine")
	}
	var arr []string
	if err := json.Unmarshal(trimmed, &arr); err == nil {
		return arr, nil
	}
	var one string
	if err := json.Unmarshal(trimmed, &one); err == nil {
		return []string{one}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &obj); err == nil {
		for _, k := range keys {
			v, ok := obj[k]
			if !ok {
				continue
			}
			var s string
			if err := json.Unmarshal(v, &s); err == nil {
				if s == "" {
					continue
				}
				return []string{s}, nil
			}
			var list []string
			if err := json.Unmarshal(v, &list); err == nil {
				return list, nil
			}
		}
		for _, k := range []string{"error", "message", "error_message"} {
			if v, ok := obj[k]; ok {
				var s string
				if err := json.Unmarshal(v, &s); err == nil && s != "" {
					return nil, fmt.Errorf("translation engine error: %s", s)
				}
			}
		}
	}
	return nil, fmt.Errorf("unrecognised translation engine response: %s", snippet(raw))
}
