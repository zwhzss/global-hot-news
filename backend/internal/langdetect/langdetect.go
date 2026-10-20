// Package langdetect provides a dependency-free language guess for news
// headlines and summaries.
//
// The aggregator mixes Chinese and English sources, and the bilingual pipeline
// needs to know which side of an article is the authoritative original before
// it can decide what still has to be translated. A full detector (CLD3,
// fastText) would mean vendoring a model; for headlines the CJK code-point ratio
// is both sufficient and completely deterministic, which also makes it testable.
package langdetect

import "unicode"

// Language codes used across the application.
const (
	ZH      = "zh"
	EN      = "en"
	Unknown = ""
)

// cjkRatioThreshold is the share of "letter-like" runes that must be CJK before
// a text is called Chinese. News headlines that embed a company or product name
// ("Apple 发布 M4 芯片") still sit far above this, while English headlines that
// quote a Chinese name ("Xi Jinping meets ...") stay far below it.
const cjkRatioThreshold = 0.20

// Detect returns ZH, EN, or Unknown for empty/degenerate input.
//
// The decision is based on the ratio of CJK ideographs to all letters and
// ideographs in the text. Digits, punctuation and whitespace are ignored so a
// number-heavy English headline is not misread.
//
// Kana or hangul anywhere in the text means Japanese or Korean: those languages
// borrow Han characters, so a pure ratio test calls them Chinese. The app only
// supports zh/en, and wrongly labelling a Japanese headline as Chinese would
// feed Japanese text to an English translator, so such text is reported as
// Unknown and left alone.
func Detect(text string) string {
	var cjk, latin int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			return Unknown
		case unicode.Is(unicode.Han, r):
			cjk++
		case unicode.IsLetter(r):
			latin++
		}
	}

	total := cjk + latin
	if total == 0 {
		return Unknown
	}
	if float64(cjk)/float64(total) >= cjkRatioThreshold {
		return ZH
	}
	return EN
}

// IsCJK reports whether the text contains any CJK ideograph at all. Used by the
// translator to skip work when it is handed text that is already in the target
// script.
func IsCJK(text string) bool {
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
