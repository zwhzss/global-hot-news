package parser

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ai-informations/internal/models"

	"golang.org/x/net/html"
)

// Factory creates parser adapters for different source types.
type Factory struct {
	httpClient *http.Client
}

func NewFactory() *Factory {
	return &Factory{
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}
}

// Parse returns a parser adapter for the given source type.
func (f *Factory) Parse(sourceType string) (Adapter, error) {
	switch strings.ToLower(sourceType) {
	case "rss", "atom":
		return &RSSAdapter{httpClient: f.httpClient}, nil
	case "html":
		return &HTMLListAdapter{httpClient: f.httpClient}, nil
	case "api", "json":
		return &JSONAPIAdapter{httpClient: f.httpClient}, nil
	case "web_rss":
		return &WebRSSAdapter{httpClient: f.httpClient}, nil
	default:
		return nil, fmt.Errorf("unsupported source type: %s", sourceType)
	}
}

// Adapter is the interface for news parsers.
type Adapter interface {
	Fetch(rawURL string) ([]models.RawNewsItem, error)
}

// RSSAdapter parses RSS/Atom feeds.
type RSSAdapter struct {
	httpClient *http.Client
}

func (a *RSSAdapter) Fetch(rawURL string) ([]models.RawNewsItem, error) {
	resp, err := a.httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch RSS: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read RSS body: %w", err)
	}

	// Feeds from Chinese sites are frequently GB2312/GBK encoded; decoding the
	// raw bytes as UTF-8 would store mojibake.
	return parseRSS(decodeBody(body, resp.Header.Get("Content-Type")), rawURL)
}

// feedXMLItem keeps an item's raw inner XML.
//
// Field-by-field struct tags cannot express this format: RSS puts the URL in
// <link>...</link> while Atom puts it in <link href="..."/>, and two struct
// fields claiming the same tag silently conflict (which is exactly how the Atom
// path lost every item). Letting encoding/xml only split the document into items
// and then extracting the fields with the tolerant, case-insensitive helpers
// handles both dialects.
type feedXMLItem struct {
	InnerXML string `xml:",innerxml"`
}

type feedXML struct {
	Channel struct {
		Items []feedXMLItem `xml:"item"`
	} `xml:"channel"`
	Entries []feedXMLItem `xml:"entry"`
}

// parseRSS reads an RSS or Atom feed.
//
// This used to run x/net/html over the document, which is an HTML parser and
// silently produced items with NO children at all: HTML's handling of
// `title`/`link` does not match XML, so every feed fell through to the
// string-based fallback. That is why no article ever had a summary and why
// publish times were almost always "just now" (which in turn broke the
// "today only" filter and starved the classifier of text). Feeds are XML, so
// they are parsed as XML; the string fallback remains for malformed documents.
func parseRSS(content, sourceURL string) ([]models.RawNewsItem, error) {
	if items := parseFeedXML(content); len(items) > 0 {
		return items, nil
	}
	return fallbackParseRSS(content, sourceURL), nil
}

func parseFeedXML(content string) []models.RawNewsItem {
	var feed feedXML
	dec := xml.NewDecoder(strings.NewReader(content))
	// Feeds are frequently not perfectly well-formed; do not lose the whole
	// document over one bad byte or an undeclared entity.
	dec.Strict = false
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) { return input, nil }
	if err := dec.Decode(&feed); err != nil {
		return nil
	}

	raw := feed.Channel.Items
	if len(raw) == 0 {
		raw = feed.Entries
	}

	items := make([]models.RawNewsItem, 0, len(raw))
	for _, it := range raw {
		if item := rawItemFromXML(it.InnerXML); item != nil {
			items = append(items, *item)
		}
	}
	return items
}

// rawItemFromXML builds one item from an item's inner XML.
func rawItemFromXML(inner string) *models.RawNewsItem {
	title := cleanTitle(extractTag(inner, "title"))
	if title == "" {
		return nil
	}

	link := strings.TrimSpace(extractTag(inner, "link"))
	if link == "" {
		link = hrefAttr(inner) // Atom: <link href="..."/>
	}
	if link == "" {
		return nil
	}

	summary := firstNonEmpty(
		extractTag(inner, "description"),
		extractTag(inner, "summary"),
		extractTag(inner, "encoded"), // content:encoded
		extractTag(inner, "content"),
	)
	pub := firstNonEmpty(
		extractTag(inner, "pubDate"),
		extractTag(inner, "pubdate"),
		extractTag(inner, "published"),
		extractTag(inner, "date"), // dc:date
		extractTag(inner, "updated"),
		extractTag(inner, "issued"),
	)

	pt, ok := parseFeedTime(pub)
	if !ok {
		return nil // no usable date — discard; see factory.go comment on parseFeedTime
	}
	return &models.RawNewsItem{
		Title:       title,
		Summary:     cleanSummary(summary),
		PublishTime: pt,
		OriginURL:   link,
		Publisher:   strings.TrimSpace(extractTag(inner, "source")),
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// fallbackParseRSS extracts items with plain string scanning, for documents the
// XML decoder rejects outright. Matching is case-insensitive so a feed that
// writes <pubdate> or <PubDate> is still understood.
func fallbackParseRSS(content, sourceURL string) []models.RawNewsItem {
	var items []models.RawNewsItem
	for _, chunk := range splitFold(content, "<item>") {
		end := indexFold(chunk, "</item>")
		if end < 0 {
			continue
		}
		line := chunk[:end]

		title := extractTag(line, "title")
		if title == "" {
			continue
		}
		link := extractTag(line, "link")
		if link == "" {
			if href := hrefAttr(line); href != "" {
				link = href
			}
		}
		if link == "" {
			link = sourceURL
		}

		pt, ok := parseFeedTime(firstNonEmpty(
			extractTag(line, "pubDate"), extractTag(line, "dc:date"), extractTag(line, "date")))
		if !ok {
			continue // no usable date — discard this item
		}
		items = append(items, models.RawNewsItem{
			Title:       cleanTitle(title),
			Summary:     cleanSummary(firstNonEmpty(extractTag(line, "description"), extractTag(line, "summary"))),
			PublishTime: pt,
			OriginURL:   link,
			Publisher:   strings.TrimSpace(extractTag(line, "source")),
		})
	}
	return items
}

func hrefAttr(s string) string {
	re := regexp.MustCompile(`(?is)<link[^>]*href\s*=\s*["']([^"']+)["']`)
	if m := re.FindStringSubmatch(s); len(m) == 2 {
		return m[1]
	}
	return ""
}

// extractTag returns the text of the first <tag>...</tag> pair, ignoring case,
// any namespace prefix (so "date" also finds "dc:date" and "encoded" finds
// "content:encoded") and any attributes on the opening tag.
func extractTag(content, tag string) string {
	prefix := `(?:[A-Za-z0-9_.-]+:)?`
	re := regexp.MustCompile(`(?is)<` + prefix + regexp.QuoteMeta(tag) + `(?:\s[^>]*)?>(.*?)</` +
		prefix + regexp.QuoteMeta(tag) + `\s*>`)
	m := re.FindStringSubmatch(content)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func splitFold(s, sep string) []string {
	var out []string
	for {
		i := indexFold(s, sep)
		if i < 0 {
			out = append(out, s)
			return out
		}
		out = append(out, s[:i])
		s = s[i+len(sep):]
	}
}

func indexFold(s, sub string) int {
	return strings.Index(strings.ToLower(s), strings.ToLower(sub))
}

// feedTimeLayouts are tried in order. Real feeds mix RFC1123 with and without a
// numeric zone, RFC822 variants and several ISO-ish shapes; a feed whose format
// is not listed here silently loses every publish time.
var feedTimeLayouts = []string{
	time.RFC1123,          // Mon, 02 Jan 2006 15:04:05 MST
	time.RFC1123Z,         // Mon, 02 Jan 2006 15:04:05 -0700
	time.RFC822,           // 02 Jan 06 15:04 MST
	time.RFC822Z,          // 02 Jan 06 15:04 -0700
	time.RFC3339,          // 2006-01-02T15:04:05Z07:00
	"2006-01-02T15:04:05", // no zone
	"2006-01-02 15:04:05", // plain
	"2006-01-02 15:04",    // truncated
	"2006-01-02",          // date only
	"Mon, 2 Jan 2006 15:04:05 MST",
	"Mon, 2 Jan 2006 15:04:05 -0700",
	"2 Jan 2006 15:04:05 MST",
}

// parseFeedTime converts a feed date into a time.
//
// Returns (zero time, false) when the raw string is empty or cannot be parsed.
// This is deliberate: an article without a publish date must NOT silently acquire
// today's timestamp, because that turns every stale story into "today's news" and
// corrupts the "today only" filter as well as the date picker. Callers must skip
// items where the returned bool is false.
func parseFeedTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	for _, layout := range feedTimeLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	// Some feeds append a redundant parenthesised zone name.
	if idx := strings.Index(raw, "("); idx > 0 {
		trimmed := strings.TrimSpace(raw[:idx])
		for _, layout := range feedTimeLayouts {
			if t, err := time.Parse(layout, trimmed); err == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// HTMLListAdapter parses HTML list pages with CSS selectors.
type HTMLListAdapter struct {
	httpClient *http.Client
}

func (a *HTMLListAdapter) Fetch(rawURL string) ([]models.RawNewsItem, error) {
	resp, err := a.httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch HTML: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read HTML body: %w", err)
	}

	return parseHTMLList(decodeBody(body, resp.Header.Get("Content-Type")), rawURL)
}

func parseHTMLList(content, baseURL string) ([]models.RawNewsItem, error) {
	doc, err := html.Parse(strings.NewReader(content))
	if err != nil {
		return nil, err
	}

	var items []models.RawNewsItem
	var extract func(*html.Node)
	extract = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if (n.Data == "a" || n.Data == "link") && hasAttr(n, "href") {
				href := getAttr(n, "href")
				text := strings.TrimSpace(getNodeText(n))
				if text != "" && len(text) > 5 && len(text) < 500 && isNewsLink(href) {
					items = append(items, models.RawNewsItem{
						Title:     cleanTitle(text),
						OriginURL: resolveURL(baseURL, href),
					})
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			extract(c)
		}
	}
	extract(doc)
	return items, nil
}

func isNewsLink(href string) bool {
	lower := strings.ToLower(href)
	// Filter out non-content links
	excludePatterns := []string{"/login", "/signup", "/admin", "/api/", ".js", ".css", ".png", ".jpg", ".svg"}
	for _, p := range excludePatterns {
		if strings.Contains(lower, p) {
			return false
		}
	}
	return true
}

// JSONAPIAdapter parses JSON API responses.
type JSONAPIAdapter struct {
	httpClient *http.Client
}

func (a *JSONAPIAdapter) Fetch(rawURL string) ([]models.RawNewsItem, error) {
	resp, err := a.httpClient.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch API: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read API body: %w", err)
	}

	return parseJSONAPI(decodeBody(body, resp.Header.Get("Content-Type")), rawURL)
}

func parseJSONAPI(content, sourceURL string) ([]models.RawNewsItem, error) {
	var items []models.RawNewsItem

	// Try to parse as array of news items
	var rawList []map[string]interface{}
	if err := json.Unmarshal([]byte(content), &rawList); err != nil {
		// Try wrapping in a result object
		var wrapper struct {
			Items  []map[string]interface{} `json:"items"`
			Data   []map[string]interface{} `json:"data"`
			News   []map[string]interface{} `json:"news"`
			Result struct {
				Items []map[string]interface{} `json:"items"`
			} `json:"result"`
		}
		if err := json.Unmarshal([]byte(content), &wrapper); err == nil {
			if len(wrapper.Items) > 0 {
				rawList = wrapper.Items
			} else if len(wrapper.Data) > 0 {
				rawList = wrapper.Data
			} else if len(wrapper.News) > 0 {
				rawList = wrapper.News
			} else if len(wrapper.Result.Items) > 0 {
				rawList = wrapper.Result.Items
			}
		}
		if len(rawList) == 0 {
			return nil, fmt.Errorf("unable to parse JSON API response")
		}
	}

	for _, item := range rawList {
		title := getStringField(item, "title", "headline", "heading")
		url := getStringField(item, "url", "link", "origin_url", "url")
		summary := getStringField(item, "summary", "description", "excerpt", "abstract")
		pubDate := getStringField(item, "publish_time", "published_at", "date", "pub_date")

		if title == "" {
			continue
		}
		if url == "" {
			url = sourceURL
		}

		pt, hasDate := time.Time{}, false
		if pubDate != "" {
			if t, err := time.Parse(time.RFC3339, pubDate); err == nil {
				pt, hasDate = t, true
			} else if t, err := time.Parse("2006-01-02", pubDate); err == nil {
				pt, hasDate = t, true
			}
		}
		if !hasDate {
			continue // no usable date — discard this item
		}

		items = append(items, models.RawNewsItem{
			Title:       cleanTitle(title),
			Summary:     summary,
			PublishTime: pt,
			OriginURL:   url,
		})
	}
	return items, nil
}

// Helper functions
func getNodeText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text string
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		text += getNodeText(c)
	}
	return text
}

func getLinkHref(n *html.Node) string {
	for _, a := range n.Attr {
		if a.Key == "href" {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func getAttr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func resolveURL(base, ref string) string {
	if strings.HasPrefix(ref, "http") {
		return ref
	}
	if strings.HasPrefix(ref, "/") {
		// Resolve relative to base
		// Simple implementation
		return base[:strings.LastIndex(base, "/")+1] + ref[1:]
	}
	return base + "/" + ref
}

func getStringField(m map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			if s, ok := v.(string); ok {
				return s
			}
		}
	}
	return ""
}

func cleanTitle(title string) string {
	// Strip CDATA wrappers
	title = stripCDATA(title)
	// Remove HTML tags
	title = strings.TrimSpace(title)
	// Collapse whitespace
	for strings.Contains(title, "  ") {
		title = strings.ReplaceAll(title, "  ", " ")
	}
	return title
}

// maxSummaryLen bounds a stored abstract. The requirement is to keep a short
// factual summary, not the article body, so anything longer is cut.
const maxSummaryLen = 2000

// cleanSummary turns a feed's description into plain text.
//
// Feed abstracts are HTML fragments (often entity-escaped, sometimes wrapped in
// CDATA) that arrive as the *text* of <description>. Storing that text verbatim
// put raw markup in the database and showed "<p>...</p>" in the UI, so the
// fragment is parsed once to extract its text.
func cleanSummary(raw string) string {
	raw = strings.TrimSpace(stripCDATA(raw))
	if raw == "" {
		return ""
	}

	// Entity-escaped markup ("&lt;p&gt;") is still markup after decoding, so
	// decode first and then parse.
	decoded := html.UnescapeString(raw)
	if strings.Contains(decoded, "<") {
		if doc, err := html.Parse(strings.NewReader(decoded)); err == nil {
			if text := strings.TrimSpace(getNodeText(doc)); text != "" {
				decoded = text
			}
		}
	}

	// Collapse every whitespace run (including the newlines feeds embed).
	decoded = strings.Join(strings.Fields(decoded), " ")
	decoded = strings.TrimSpace(decoded)

	if len([]rune(decoded)) > maxSummaryLen {
		runes := []rune(decoded)
		decoded = strings.TrimSpace(string(runes[:maxSummaryLen]))
	}
	return decoded
}

func stripCDATA(s string) string {
	// Remove <![CDATA[ ... ]]> wrappers
	for strings.Contains(s, "<![CDATA[") {
		start := strings.Index(s, "<![CDATA[")
		end := strings.Index(s, "]]>")
		if end > start {
			s = s[:start] + s[start+len("<![CDATA["):end] + s[end+3:]
		} else {
			break
		}
	}
	return s
}

// WebRSSAdapter fetches a webpage through an RSS conversion service (e.g. RSSHub)
// and parses the resulting RSS/Atom feed. This lets users subscribe to sites
// that do not publish official RSS feeds — the original URL is the "source",
// the conversion service is just a transparent bridge.
type WebRSSAdapter struct {
	httpClient *http.Client
}

// rsshubInstance is the default RSSHub instance used when the user has not
// configured a custom one via the rsshub_instance system setting.
const defaultRSSHubInstance = "https://rsshub.app"

func (a *WebRSSAdapter) Fetch(rawURL string) ([]models.RawNewsItem, error) {
	rssHubURL, err := a.buildRSSHubURL(rawURL)
	if err != nil {
		return nil, fmt.Errorf("web_rss: failed to build feed URL: %w", err)
	}

	resp, err := a.httpClient.Get(rssHubURL)
	if err != nil {
		return nil, fmt.Errorf("web_rss: failed to fetch from RSS hub: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("web_rss: failed to read body: %w", err)
	}

	// The feed from the conversion service may contain the original URL in a
	// custom <origin> tag or in the item description; we pass it through so the
	// article's OriginURL points at the real page, not the RSS hub intermediary.
	feedURL := rssHubURL
	items, err := parseRSS(decodeBody(body, resp.Header.Get("Content-Type")), rawURL)
	if err != nil {
		return nil, fmt.Errorf("web_rss: failed to parse feed: %w", err)
	}
	// Rewrite origin URLs: if the feed item links back to the conversion
	// service, replace with the original webpage URL so the reader opens the
	// real article.
	for i := range items {
		if items[i].OriginURL == feedURL || strings.Contains(items[i].OriginURL, "rsshub.app") {
			items[i].OriginURL = rawURL
		}
	}
	return items, nil
}

// buildRSSHubURL tries common conversion patterns for the given webpage URL.
// It returns the RSS feed URL and the original URL (for origin rewriting).
func (a *WebRSSAdapter) buildRSSHubURL(pageURL string) (string, error) {
	// 1. Try to read a custom RSSHub instance from system settings.
	//    (The setting is injected at construction time via SetRSSHubInstance.)
	inst := defaultRSSHubInstance
	if rsshubInst != "" {
		inst = rsshubInst
	}

	// 2. Try to derive an RSSHub route from the page URL.
	//    Common patterns:
	//    - toutiao.com/... -> toutiao/topic/{topicId}
	//    - weibo.com/...   -> weibo/user/{userId}
	//    - zhihu.com/...   -> zhihu/people/activities/{id}
	//    For unknown domains we fall back to a generic route that some RSSHub
	//    instances support: /route?pattern=<encoded_path>
	route := deriveRSSHubRoute(pageURL)
	if route == "" {
		return "", fmt.Errorf("web_rss: cannot derive RSSHub route for %s; please add a custom route in source settings", pageURL)
	}
	return inst + "/" + route, nil
}

// deriveRSSHubRoute extracts an RSSHub-compatible route from common Chinese
// news and social-media URLs. Unknown patterns return "".
func deriveRSSHubRoute(url string) string {
	lower := strings.ToLower(url)
	switch {
	case strings.Contains(lower, "toutiao.com"):
		// Extract topic ID from urls like .../topic/23456789/
		if idx := strings.Index(lower, "/topic/"); idx >= 0 {
			rest := lower[idx+len("/topic/"):]
			if end := strings.IndexAny(rest, "/?#"); end > 0 {
				return "toutiao/topic/" + rest[:end]
			}
		}
	case strings.Contains(lower, "weibo.com"):
		if idx := strings.Index(lower, "/u/"); idx >= 0 {
			rest := lower[idx+len("/u/"):]
			if end := strings.IndexAny(rest, "/?#"); end > 0 {
				return "weibo/user/" + rest[:end]
			}
		}
	case strings.Contains(lower, "zhihu.com"):
		if idx := strings.Index(lower, "/people/"); idx >= 0 {
			rest := lower[idx+len("/people/"):]
			if end := strings.IndexAny(rest, "/?#"); end > 0 {
				return "zhihu/people/activities/" + rest[:end]
			}
		}
	case strings.Contains(lower, "bilibili.com"):
		if idx := strings.Index(lower, "/spaceUp"); idx >= 0 {
			// spaceUp?mid=123 -> bilibili/user/123
			if qidx := strings.Index(lower[idx:], "?"); qidx >= 0 {
				if midIdx := strings.Index(lower[idx+qidx:], "mid="); midIdx >= 0 {
					mid := lower[idx+qidx+midIdx+4:]
					if e := strings.IndexAny(mid, "&#"); e > 0 {
						mid = mid[:e]
					}
					return "bilibili/user/arc/" + mid
				}
			}
		}
	}
	// Fallback: use the generic /route endpoint if the RSSHub instance supports it.
	// This requires the user to configure the exact route pattern in the source's
	// cronExpression field (reused as a "custom route" field).
	return ""
}

// rsshubInst is an optional override for the default RSSHub instance, set via
// the rsshub_instance system setting. Initialized at startup from the DB.
var rsshubInst string

// SetRSSHubInstance overrides the default RSSHub instance URL.
func SetRSSHubInstance(url string) {
	if strings.TrimSpace(url) != "" {
		rsshubInst = strings.TrimRight(url, "/")
	}
}
