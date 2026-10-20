package parser

import (
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// China News politics (www.chinanews.com.cn/rss/politics.xml) served a
// GB2312-encoded feed whose XML declaration the old code ignored: the raw GBK
// bytes were stored as-is, so ten articles landed in the database as invalid
// UTF-8, were classified as English (no Han characters left to detect), and were
// then handed to the translator in the wrong direction.

const gbkTitle = "高尚全：公共服务领域副部级成果公共资源浪费"

func toGBK(t *testing.T, s string) []byte {
	t.Helper()
	out, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte(s))
	if err != nil {
		t.Fatalf("encode to GBK: %v", err)
	}
	return out
}

// TestDecodeBodyHonoursXMLDeclaration is the exact shape of the broken feed.
func TestDecodeBodyHonoursXMLDeclaration(t *testing.T) {
	xml := `<?xml version="1.0" encoding="gb2312"?><rss><channel><item><title>` +
		gbkTitle + `</title><link>https://example.com/a</link><pubDate>Tue, 26 Sep 2026 08:00:00 GMT</pubDate></item></channel></rss>`

	// Build the body as bytes: declaration in ASCII, content in GBK.
	prefix := `<?xml version="1.0" encoding="gb2312"?><rss><channel><item><title>`
	suffix := `</title><link>https://example.com/a</link><pubDate>Tue, 26 Sep 2026 08:00:00 GMT</pubDate></item></channel></rss>`
	body := append([]byte(prefix), toGBK(t, gbkTitle)...)
	body = append(body, []byte(suffix)...)
	_ = xml

	got := decodeBody(body, "")
	if !strings.Contains(got, gbkTitle) {
		t.Fatalf("decoded body does not contain the original Chinese title:\n%q", got)
	}

	// And end to end: the parser must produce the readable title.
	items, err := parseRSS(got, "http://example.com/feed.xml")
	if err != nil {
		t.Fatalf("parseRSS: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("parsed %d items, want 1", len(items))
	}
	if items[0].Title != gbkTitle {
		t.Fatalf("title = %q, want %q", items[0].Title, gbkTitle)
	}
}

// TestDecodeBodyHonoursContentTypeHeader covers the sites that declare the
// charset only in the HTTP header.
func TestDecodeBodyHonoursContentTypeHeader(t *testing.T) {
	body := toGBK(t, gbkTitle)
	got := decodeBody(body, "text/xml; charset=GBK")
	if got != gbkTitle {
		t.Fatalf("got %q, want %q", got, gbkTitle)
	}
}

// TestDecodeBodyFallsBackToGBKWhenUndeclared covers a feed with no declaration
// at all: invalid UTF-8 from a Chinese source is a GBK-family feed in practice.
func TestDecodeBodyFallsBackToGBKWhenUndeclared(t *testing.T) {
	body := toGBK(t, gbkTitle)
	got := decodeBody(body, "")
	if got != gbkTitle {
		t.Fatalf("got %q, want %q", got, gbkTitle)
	}
}

// TestDecodeBodyLeavesUTF8Alone guards the common path.
func TestDecodeBodyLeavesUTF8Alone(t *testing.T) {
	body := []byte(`<?xml version="1.0" encoding="utf-8"?><title>` + gbkTitle + `</title>`)
	got := decodeBody(body, "application/rss+xml; charset=utf-8")
	if !strings.Contains(got, gbkTitle) {
		t.Fatalf("UTF-8 body was altered:\n%q", got)
	}
}

// TestDecodeBodyEmpty keeps the empty-response path from panicking.
func TestDecodeBodyEmpty(t *testing.T) {
	if got := decodeBody(nil, ""); got != "" {
		t.Fatalf("decodeBody(nil) = %q, want empty", got)
	}
}

// TestCharsetHelpers pins the small parsers the decoder relies on.
func TestCharsetHelpers(t *testing.T) {
	if got := charsetFromHeader("text/html; charset=gb2312"); got != "gb2312" {
		t.Fatalf("charsetFromHeader = %q", got)
	}
	if got := charsetFromHeader("text/html"); got != "" {
		t.Fatalf("charsetFromHeader without charset = %q", got)
	}
	if got := charsetFromDocument([]byte(`<?xml version="1.0" encoding="GBK"?>`)); got != "GBK" {
		t.Fatalf("charsetFromDocument xml = %q", got)
	}
	if got := charsetFromDocument([]byte(`<meta charset="big5">`)); got != "big5" {
		t.Fatalf("charsetFromDocument meta = %q", got)
	}
	if got := charsetFromDocument([]byte(`<meta http-equiv="Content-Type" content="text/html; charset=gbk">`)); got != "gbk" {
		t.Fatalf("charsetFromDocument http-equiv = %q", got)
	}
	if got := charsetFromDocument([]byte(`<rss><channel/></rss>`)); got != "" {
		t.Fatalf("charsetFromDocument undeclared = %q", got)
	}
}
