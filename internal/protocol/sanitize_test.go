package protocol

import "testing"

func TestSanitizeAlias(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{"plain ascii", "Bob's iPhone", "Bob's iPhone"},
		{"strips ESC/ANSI", "Bob\x1b[31mRED\x1b[0m", "Bob[31mRED[0m"},
		{"strips control chars", "a\x00b\x07c\x7fd", "abcd"},
		{"trims surrounding whitespace after stripping", "  hi  ", "hi"},
		{"preserves multi-script text", "デバイス-ünïcödé-🎉", "デバイス-ünïcödé-🎉"},
		{"empty stays empty", "", ""},
		{"whitespace only becomes empty", "\x00   \x00", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeAlias(tc.input)
			if got != tc.want {
				t.Fatalf("SanitizeAlias(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestSanitizeAliasLengthCap(t *testing.T) {
	long := ""
	for i := 0; i < MaxAliasRunes+50; i++ {
		long += "a"
	}
	got := SanitizeAlias(long)
	if len([]rune(got)) != MaxAliasRunes {
		t.Fatalf("expected length %d, got %d", MaxAliasRunes, len([]rune(got)))
	}
}
