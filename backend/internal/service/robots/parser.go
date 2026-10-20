package robots

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Parser parses robots.txt and checks if a URL is allowed.
type Parser struct {
	mu       sync.Mutex
	cache    map[string]*robotsData // domain -> parsed data
	httpClient *http.Client
}

type robotsData struct {
	allowPaths []string
	denyPaths  []string
	crawlDelay int // seconds
	fetchedAt  time.Time
}

// NewParser creates a new robots.txt parser.
func NewParser() *Parser {
	return &Parser{
		cache: make(map[string]*robotsData),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// IsAllowed checks if the given URL is allowed by robots.txt.
func (p *Parser) IsAllowed(rawURL string) (bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("invalid URL: %w", err)
	}

	domain := u.Scheme + "://" + u.Host
	p.mu.Lock()
	defer p.mu.Unlock()

	data, err := p.getRobotsData(domain)
	if err != nil {
		return true, nil // If we can't fetch robots.txt, allow by default (gentle fallback)
	}

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}

	// Check deny rules first
	for _, pattern := range data.denyPaths {
		if matchPattern(path, pattern) {
			return false, nil
		}
	}

	// Check allow rules
	for _, pattern := range data.allowPaths {
		if matchPattern(path, pattern) {
			return true, nil
		}
	}

	return true, nil // Default: allowed
}

func (p *Parser) getRobotsData(domain string) (*robotsData, error) {
	if data, ok := p.cache[domain]; ok && time.Since(data.fetchedAt) < 1*time.Hour {
		return data, nil
	}

	robotsURL := domain + "/robots.txt"
	resp, err := p.httpClient.Get(robotsURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch robots.txt: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("robots.txt returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read robots.txt: %w", err)
	}

	data := parseRobotsTxt(string(body))
	data.fetchedAt = time.Now()
	p.cache[domain] = data
	return data, nil
}

func parseRobotsTxt(content string) *robotsData {
	data := &robotsData{}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(strings.ToLower(parts[0]))
		value := strings.TrimSpace(parts[1])

		switch key {
		case "disallow":
			data.denyPaths = append(data.denyPaths, value)
		case "allow":
			data.allowPaths = append(data.allowPaths, value)
		case "crawl-delay":
			if d, err := strconv.Atoi(value); err == nil {
				data.crawlDelay = d
			}
		}
	}
	return data
}

func matchPattern(path, pattern string) bool {
	// Simple wildcard matching: * matches anything, ? matches single char
	pat := strings.ToLower(pattern)
	p := strings.ToLower(path)

	if pat == "*" {
		return true
	}

	// Convert pattern to regex-like matching
	patParts := strings.Split(pat, "*")
	pathParts := strings.Split(p, "*")

	if len(patParts) == 1 {
		// No wildcard - exact match or suffix match
		if strings.HasSuffix(pat, "/") {
			return strings.HasPrefix(p, pat)
		}
		return p == pat
	}

	// Handle wildcards
	idx := 0
	for i, part := range patParts {
		if i == 0 {
			if !strings.HasPrefix(p[idx:], part) {
				return false
			}
			idx += len(part)
		} else {
			nextIdx := strings.Index(p[idx:], part)
			if nextIdx == -1 {
				return false
			}
			idx += nextIdx + len(part)
		}
	}

	// Check remaining path matches last pattern part
	if len(patParts)-1 < len(pathParts) {
		lastPat := patParts[len(patParts)-1]
		remaining := p[idx:]
		return strings.HasSuffix(remaining, lastPat) || lastPat == ""
	}
	return true
}
