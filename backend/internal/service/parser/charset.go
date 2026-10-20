package parser

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// Chinese news sites still serve GB2312/GBK and Big5 feeds, and a few announce
// the encoding only inside the XML declaration. Decoding those bytes as UTF-8
// produces the replacement character, which is how ten China News politics
// articles ended up stored as unreadable text (and then got classified into the
// wrong language, because the mojibake no longer contained any Han characters).
//
// Every adapter therefore funnels its response body through decodeBody.

var (
	// metaCharsetRe matches <meta charset="gbk"> and the http-equiv form.
	metaCharsetRe = regexp.MustCompile(`(?i)charset\s*=\s*["']?\s*([a-z0-9_\-]+)`)
	// xmlEncodingRe matches the XML declaration's encoding attribute.
	xmlEncodingRe = regexp.MustCompile(`(?i)<\?xml[^>]*encoding\s*=\s*["']([a-z0-9_\-]+)["']`)
)

// decodeBody converts a response body to UTF-8. contentType is the HTTP
// Content-Type header, which wins over the in-document declaration.
func decodeBody(body []byte, contentType string) string {
	if len(body) == 0 {
		return ""
	}

	if enc := lookupEncoding(charsetFromHeader(contentType)); enc != nil {
		if out, err := enc.NewDecoder().Bytes(body); err == nil {
			return string(out)
		}
	}

	if enc := lookupEncoding(charsetFromDocument(body)); enc != nil {
		if out, err := enc.NewDecoder().Bytes(body); err == nil {
			return string(out)
		}
	}

	// Nothing declared. If the bytes are already valid UTF-8 we are done; if not,
	// the overwhelmingly likely case for a Chinese source is a GBK-family feed
	// that simply forgot to declare itself. GB18030 is a strict superset of GBK
	// and GB2312, so one decoder covers all three.
	if utf8.Valid(body) {
		return string(body)
	}
	if out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(body); err == nil {
		return string(out)
	}
	return strings.ToValidUTF8(string(body), "")
}

// charsetFromHeader extracts the charset from a Content-Type header.
func charsetFromHeader(contentType string) string {
	idx := strings.Index(strings.ToLower(contentType), "charset=")
	if idx < 0 {
		return ""
	}
	value := contentType[idx+len("charset="):]
	value = strings.TrimSpace(value)
	value = strings.Trim(value, `"'`)
	if end := strings.IndexAny(value, ";, "); end >= 0 {
		value = value[:end]
	}
	return value
}

// charsetFromDocument reads the declared encoding from the first part of the
// document, checking the XML declaration before any <meta> tag because a feed
// usually has the former and an HTML page the latter.
func charsetFromDocument(body []byte) string {
	head := body
	if len(head) > 4096 {
		head = head[:4096]
	}
	text := string(head)

	if m := xmlEncodingRe.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	if m := metaCharsetRe.FindStringSubmatch(text); len(m) == 2 {
		return m[1]
	}
	return ""
}

// lookupEncoding maps a charset name to a decoder. Nil means "already UTF-8 or
// unknown", in which case decodeBody falls back to the byte-level check.
func lookupEncoding(name string) encoding.Encoding {
	switch normalizeCharset(name) {
	case "gb2312", "gbk", "gb18030", "csgb2312", "x-gbk", "ms936", "cp936":
		return simplifiedchinese.GB18030
	case "big5", "big-5", "csbig5", "cp950":
		return traditionalchinese.Big5
	default:
		return nil
	}
}

func normalizeCharset(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.Trim(name, `"'`)
	return name
}
