package tui

import (
	"errors"
	"strings"
	"testing"
)

func TestParseKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"arrow up", "\x1b[A", []string{"up"}},
		{"arrow down", "\x1b[B", []string{"down"}},
		{"application-mode arrow up", "\x1bOA", []string{"up"}},
		{"application-mode arrow down", "\x1bOB", []string{"down"}},
		{"home", "\x1b[H", []string{"home"}},
		{"end", "\x1b[F", []string{"end"}},
		{"application-mode home and end", "\x1bOH\x1bOF", []string{"home", "end"}},
		{"home (vt style)", "\x1b[1~", []string{"home"}},
		{"end (vt style)", "\x1b[4~", []string{"end"}},
		{"page up", "\x1b[5~", []string{"pgup"}},
		{"page down", "\x1b[6~", []string{"pgdn"}},
		{"arrow with modifier", "\x1b[1;5A", []string{"up"}},
		{"page up with modifier", "\x1b[5;2~", []string{"pgup"}},
		{"keypad home and end", "\x1b[7~\x1b[8~", []string{"home", "end"}},
		{"shift-tab is ignored", "\x1b[Z", nil},
		{"a sequence with an intermediate byte is consumed whole", "\x1b[2 qx", []string{"x"}},
		{"an invalid sequence is skipped, the rest is parsed", "\x1b[1\x01x", []string{"x"}},
		{"escape alone", "\x1b", []string{"esc"}},
		{"escape then a key", "\x1bx", []string{"esc", "x"}},
		{"carriage return", "\r", []string{"enter"}},
		{"newline", "\n", []string{"enter"}},
		{"tab", "\t", []string{"tab"}},
		{"DEL is backspace", "\x7f", []string{"backspace"}},
		{"BS is backspace", "\x08", []string{"backspace"}},
		{"ctrl-c", "\x03", []string{"ctrl-c"}},
		{"printable run", "abc", []string{"a", "b", "c"}},
		{"keys around a sequence", "j\x1b[Bk", []string{"j", "down", "k"}},
		{"control and non-ASCII bytes are ignored", "\x00\x01\xc3\xa9", nil},
		{"empty input", "", nil},

		// Keys BootLore does not use must vanish: they must neither act as
		// Escape (closing the detail pane) nor leak their tail into the
		// filter text.
		{"delete key is ignored", "\x1b[3~", nil},
		{"insert key is ignored", "\x1b[2~", nil},
		{"function key is ignored", "\x1b[15~", nil},
		{"unknown sequence does not leak its tail", "\x1b[3~q", []string{"q"}},
		{"incomplete sequence is dropped", "\x1b[", nil},
		{"incomplete parameters are dropped", "\x1b[1;", nil},
		{"incomplete application sequence", "\x1bO", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseKeys([]byte(tc.in))
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("parseKeys(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestReadKeysDeliversKeysAndClosesOnEOF(t *testing.T) {
	ch := make(chan string, 8)
	go readKeys(strings.NewReader("q\x1b[Ax"), ch)

	var got []string
	for k := range ch {
		got = append(got, k)
	}
	if want := "q|up|x"; strings.Join(got, "|") != want {
		t.Errorf("readKeys delivered %q, want %q", got, want)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// A failing terminal must end the key stream, so the run loop can leave.
func TestReadKeysClosesOnReadError(t *testing.T) {
	ch := make(chan string, 1)
	go readKeys(failingReader{}, ch)
	if _, ok := <-ch; ok {
		t.Error("the channel should be closed after a read error")
	}
}
