package tui

import (
	"strings"
	"testing"
)

func TestEncodeFrame(t *testing.T) {
	f := Frame{Width: 4, Height: 2, Lines: []Line{{Text: "ab"}, {Text: "cd", Reverse: true}}}
	want := "\x1b[H" + "ab  \x1b[K" + "\r\n" + "\x1b[7mcd  \x1b[27m\x1b[K"
	if got := encodeFrame(f); got != want {
		t.Errorf("encodeFrame()\n got = %q\nwant = %q", got, want)
	}
}

// A newline after the bottom row would scroll the alternate screen up one line
// on every redraw.
func TestEncodeFrameDoesNotScroll(t *testing.T) {
	f := Frame{Width: 5, Height: 3, Lines: []Line{{Text: "a"}, {Text: "b"}, {Text: "c"}}}
	got := encodeFrame(f)
	if n := strings.Count(got, "\r\n"); n != 2 {
		t.Errorf("3 rows need exactly 2 separators, got %d in %q", n, got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("the frame must not end with a newline: %q", got)
	}
}

// Padding counts characters, not bytes, so accented rows still span the width.
func TestEncodeFramePadsByCharacters(t *testing.T) {
	f := Frame{Width: 5, Height: 1, Lines: []Line{{Text: "é"}}}
	if got, want := encodeFrame(f), "\x1b[Hé    \x1b[K"; got != want {
		t.Errorf("encodeFrame() = %q, want %q", got, want)
	}
}

func TestEncodeEmptyFrame(t *testing.T) {
	if got := encodeFrame(Frame{}); got != "\x1b[H" {
		t.Errorf("encodeFrame(empty) = %q, want just the cursor-home sequence", got)
	}
}
