package parser

import (
	"strings"
	"testing"
)

// The RSS path used to drop every abstract and every publish date:
// `<description>` was never read at all, and tag names were compared
// case-sensitively against names that x/net/html had already lowercased
// ("pubDate" never matched "pubdate"). The result was 100% empty summaries in
// the database and publish times that were always "just now", which in turn
// broke the "today only" board filter and starved the classifier of text.

const rssFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">
  <channel>
    <title>Example</title>
    <item>
      <title>央行维持利率不变</title>
      <link>https://example.com/a</link>
      <description><![CDATA[<p>中国人民银行周二宣布<strong>维持</strong>基准利率不变。</p>]]></description>
      <pubDate>Tue, 26 Sep 2026 08:00:00 GMT</pubDate>
    </item>
    <item>
      <title>Fed holds rates</title>
      <link>https://example.com/b</link>
      <description>   The Federal Reserve left rates unchanged.  </description>
      <dc:date>2026-09-26T09:30:00Z</dc:date>
    </item>
  </channel>
</rss>`

func TestParseRSSExtractsSummaryAndPublishTime(t *testing.T) {
	items, err := parseRSS(rssFeed, "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("parseRSS: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("parsed %d items, want 2", len(items))
	}

	if items[0].Title != "央行维持利率不变" {
		t.Fatalf("title = %q", items[0].Title)
	}
	// The CDATA-wrapped HTML abstract must be stored as plain text.
	if items[0].Summary != "中国人民银行周二宣布维持基准利率不变。" {
		t.Fatalf("summary = %q, want the tag-stripped text", items[0].Summary)
	}
	if items[0].PublishTime.IsZero() || items[0].PublishTime.Year() != 2026 {
		t.Fatalf("publish time = %v, want 2026", items[0].PublishTime)
	}
	if items[0].PublishTime.UTC().Format("2006-01-02 15:04") != "2026-09-26 08:00" {
		t.Fatalf("publish time = %v, want 2026-09-26 08:00 UTC", items[0].PublishTime.UTC())
	}

	// Lowercase `dc:date` must be honoured too, and whitespace collapsed.
	if items[1].Summary != "The Federal Reserve left rates unchanged." {
		t.Fatalf("summary = %q", items[1].Summary)
	}
	if items[1].PublishTime.UTC().Format("2006-01-02 15:04") != "2026-09-26 09:30" {
		t.Fatalf("publish time = %v, want 2026-09-26 09:30 UTC", items[1].PublishTime.UTC())
	}
}

// TestParseRSSHandlesAtomEntries covers Atom, which uses <entry>/<summary> and
// keeps the link in an attribute.
func TestParseRSSHandlesAtomEntries(t *testing.T) {
	atom := `<?xml version="1.0" encoding="utf-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <title>Quantum chip milestone</title>
    <link href="https://example.com/q"/>
    <summary>A new quantum processor was demonstrated.</summary>
    <updated>2026-09-27T10:15:00Z</updated>
  </entry>
</feed>`

	items, err := parseRSS(atom, "https://example.com/atom.xml")
	if err != nil {
		t.Fatalf("parseRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("parsed %d items, want 1", len(items))
	}
	if items[0].OriginURL != "https://example.com/q" {
		t.Fatalf("link = %q, want the href attribute", items[0].OriginURL)
	}
	if items[0].Summary != "A new quantum processor was demonstrated." {
		t.Fatalf("summary = %q", items[0].Summary)
	}
	if items[0].PublishTime.UTC().Format("2006-01-02 15:04") != "2026-09-27 10:15" {
		t.Fatalf("publish time = %v", items[0].PublishTime.UTC())
	}
}

// TestParseRSSFallsBackToContentWhenNoSummary covers feeds that only ship
// content:encoded.
func TestParseRSSFallsBackToContentWhenNoSummary(t *testing.T) {
	feed := `<rss version="2.0" xmlns:content="http://purl.org/rss/1.0/modules/content/">
  <channel><item>
    <title>Only content</title>
    <link>https://example.com/c</link>
    <pubDate>Tue, 26 Sep 2026 08:00:00 GMT</pubDate>
    <content:encoded>&lt;p&gt;Body text here.&lt;/p&gt;</content:encoded>
  </item></channel>
</rss>`

	items, err := parseRSS(feed, "https://example.com/feed.xml")
	if err != nil {
		t.Fatalf("parseRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("parsed %d items, want 1", len(items))
	}
	if items[0].Summary != "Body text here." {
		t.Fatalf("summary = %q, want the decoded text", items[0].Summary)
	}
}

func TestParseRSSFallbackPathAlsoExtractsSummary(t *testing.T) {
	// A payload the HTML parser finds no items in (no well-formed structure)
	// forces the string-based fallback.
	raw := `<item><title>Fallback title</title><link>https://example.com/f</link>` +
		`<description>Fallback summary</description><pubDate>Tue, 26 Sep 2026 08:00:00 GMT</pubDate></item>`

	items := fallbackParseRSS(raw, "https://example.com/feed.xml")
	if len(items) != 1 {
		t.Fatalf("parsed %d items, want 1", len(items))
	}
	if items[0].Summary != "Fallback summary" {
		t.Fatalf("summary = %q", items[0].Summary)
	}
	if items[0].PublishTime.UTC().Format("2006-01-02") != "2026-09-26" {
		t.Fatalf("publish time = %v", items[0].PublishTime.UTC())
	}
}

func TestParseFeedTimeLayouts(t *testing.T) {
	cases := []struct {
		in   string
		want string // formatted in UTC
	}{
		{"Tue, 26 Sep 2026 08:00:00 GMT", "2026-09-26 08:00"},
		{"Tue, 26 Sep 2026 08:00:00 +0800", "2026-09-26 00:00"},
		{"2026-09-26T08:00:00Z", "2026-09-26 08:00"},
		{"2026-09-26T08:00:00", "2026-09-26 08:00"},
		{"2026-09-26 08:00:00", "2026-09-26 08:00"},
		{"2026-09-26", "2026-09-26 00:00"},
		{"Tue, 26 Sep 2026 08:00:00 GMT (UTC)", "2026-09-26 08:00"},
	}
	for _, tc := range cases {
		got, ok := parseFeedTime(tc.in)
		if !ok {
			t.Fatalf("parseFeedTime(%q) returned not-ok", tc.in)
		}
		gotFmt := got.UTC().Format("2006-01-02 15:04")
		if gotFmt != tc.want {
			t.Fatalf("parseFeedTime(%q) = %s, want %s", tc.in, gotFmt, tc.want)
		}
	}

	// An unparseable value must return zero time and not-ok so callers discard
	// the item instead of treating it as "just now".
	if _, ok := parseFeedTime("not a date"); ok {
		t.Fatal("unparseable date should return not-ok")
	}
	if _, ok := parseFeedTime(""); ok {
		t.Fatal("empty date should return not-ok")
	}
}

func TestCleanSummaryBoundsLength(t *testing.T) {
	long := strings.Repeat("摘要", 2000)
	got := cleanSummary(long)
	if len([]rune(got)) != maxSummaryLen {
		t.Fatalf("length = %d runes, want %d", len([]rune(got)), maxSummaryLen)
	}
	if got := cleanSummary("   "); got != "" {
		t.Fatalf("blank summary = %q, want empty", got)
	}
}
