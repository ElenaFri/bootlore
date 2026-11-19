package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		w    int
		want string
	}{
		{"hello", 10, "hello"},
		{"hello", 5, "hello"},
		{"hello world", 8, "hello w…"},
		{"héllo wörld", 8, "héllo w…"}, // widths count characters, not bytes
		{"hello world", 2, "h…"},
		{"hello world", 1, "…"},
		{"hello world", 0, ""},
		{"hello world", -3, ""},
		{"", 5, ""},
	}
	for _, tc := range cases {
		got := truncate(tc.in, tc.w)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("truncate(%q, %d) produced invalid UTF-8: %q", tc.in, tc.w, got)
		}
	}
}

func TestWrap(t *testing.T) {
	cases := []struct {
		name string
		in   string
		w    int
		want []string
	}{
		{"fits", "short", 10, []string{"short"}},
		{"breaks at a space", "the quick brown fox", 10, []string{"the quick", "brown fox"}},
		{"breaks exactly at the width", "abcde fghij", 5, []string{"abcde", "fghij"}},
		{"hard-breaks a word longer than the width", "abcdefghijkl", 5, []string{"abcde", "fghij", "kl"}},
		{"counts characters, not bytes", "éèêëàâ", 4, []string{"éèêë", "àâ"}},
		{"keeps paragraphs", "one\ntwo", 10, []string{"one", "two"}},
		{"keeps an empty paragraph", "a\n\nb", 10, []string{"a", "", "b"}},
		{"empty string", "", 10, []string{""}},
		{"no usable width returns the text whole", "abc def", 0, []string{"abc def"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := wrap(tc.in, tc.w)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("wrap(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
			}
			for _, line := range got {
				if !utf8.ValidString(line) {
					t.Errorf("wrap produced invalid UTF-8: %q", line)
				}
				if tc.w > 0 && utf8.RuneCountInString(line) > tc.w {
					t.Errorf("line %q is wider than %d", line, tc.w)
				}
			}
		})
	}
}
