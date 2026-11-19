package tui

import (
	"strings"
	"unicode/utf8"
)

// truncate shortens s to at most w characters, ending with "…" when it had to
// cut. Widths count characters (runes), not bytes: cutting inside a multi-byte
// character would emit invalid UTF-8 and garble the terminal.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string([]rune(s)[:w-1]) + "…"
}

// wrap splits s into lines of at most w characters. Paragraphs (newlines) are
// kept, lines break at a space when one is available and hard-break otherwise.
// Spacing is never rewritten, because the LOG text must stay faithful to the
// original message. A width below 1 returns the text unchanged.
func wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	var out []string
	for _, paragraph := range strings.Split(s, "\n") {
		out = append(out, wrapParagraph(paragraph, w)...)
	}
	return out
}

func wrapParagraph(p string, w int) []string {
	r := []rune(p)
	var lines []string
	for len(r) > w {
		brk := -1
		for i := w; i > 0; i-- { // the last space within reach (r[w] may be it)
			if r[i] == ' ' {
				brk = i
				break
			}
		}
		if brk > 0 {
			lines = append(lines, string(r[:brk]))
			r = r[brk+1:]
		} else {
			lines = append(lines, string(r[:w]))
			r = r[w:]
		}
	}
	return append(lines, string(r))
}
