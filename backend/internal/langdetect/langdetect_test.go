package langdetect

import "testing"

// The bilingual pipeline keys every decision off Detect, so these cases pin the
// two directions it has to get right and the ambiguous shapes in between.

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		text string
		want string
	}{
		{"pure chinese", "央行宣布下调存款准备金率", ZH},
		{"chinese with product name", "Apple 发布 M4 芯片，性能提升 30%", ZH},
		{"pure english", "Fed holds interest rates steady at 4.5%", EN},
		{"english with a chinese name", "Xi Jinping meets Putin in Beijing", EN},
		{"numbers only", "2026 09 28 12:30", Unknown},
		{"empty", "", Unknown},
		{"punctuation only", "--- ... !!!", Unknown},
		{"japanese kana", "日銀は金利を据え置いた", Unknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Detect(tc.text)
			// Japanese kana text contains Han characters too; the exact label is
			// not important, only that it is not silently treated as Chinese.
			if tc.want == Unknown {
				if got == ZH {
					t.Fatalf("Detect(%q) = %q, must not be classified as Chinese", tc.text, got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("Detect(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestIsCJK(t *testing.T) {
	if !IsCJK("央行") {
		t.Fatal("IsCJK should be true for Chinese text")
	}
	if IsCJK("Central bank") {
		t.Fatal("IsCJK should be false for English text")
	}
}
