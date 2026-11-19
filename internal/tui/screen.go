package tui

import (
	"strings"
	"unicode/utf8"
)

// Terminal control sequences used around a TUI session.
const (
	// enterScreen switches to the alternate screen, hides the cursor and clears.
	enterScreen = "\x1b[?1049h\x1b[?25l\x1b[2J"
	// leaveScreen shows the cursor again and returns to the normal screen.
	leaveScreen = "\x1b[?25h\x1b[?1049l"
)

// encodeFrame turns a rendered Frame into the bytes to write to the terminal.
//
// Rows are separated by CR LF but the last row is not followed by one: a final
// newline on the bottom row would scroll the screen up by a line on every
// redraw. Each row is padded to the full width (so reverse video spans it) and
// ends with "erase to end of line" to wipe remnants of the previous frame. Only
// reverse video is used for emphasis, which stays readable on monochrome
// terminals.
func encodeFrame(f Frame) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, l := range f.Lines {
		if i > 0 {
			b.WriteString("\r\n")
		}
		if l.Reverse {
			b.WriteString("\x1b[7m")
		}
		b.WriteString(l.Text)
		for n := utf8.RuneCountInString(l.Text); n < f.Width; n++ {
			b.WriteByte(' ')
		}
		if l.Reverse {
			b.WriteString("\x1b[27m")
		}
		b.WriteString("\x1b[K")
	}
	return b.String()
}
