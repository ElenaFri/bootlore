package tui

import "io"

// readKeys turns terminal input into key names for the model and closes out
// when the input ends or fails. Terminals deliver an escape sequence as a
// single burst, so each read is parsed on its own.
func readKeys(in io.Reader, out chan<- string) {
	defer close(out)
	buf := make([]byte, 64)
	for {
		n, err := in.Read(buf)
		for _, key := range parseKeys(buf[:n]) {
			out <- key
		}
		if err != nil {
			return
		}
	}
}

// parseKeys converts raw terminal input bytes into the key names the Model
// understands: "up", "down", "home", "end", "pgup", "pgdn", "enter", "esc",
// "tab", "backspace", "ctrl-c", or a single printable ASCII character.
//
// Escape sequences are consumed whole. Ones BootLore has no use for (Delete,
// Insert, function keys, ...) are dropped: they must neither act as Escape nor
// leak their trailing characters as typed keys.
func parseKeys(b []byte) []string {
	var keys []string
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			key, n := parseEscape(b[i:])
			if key != "" {
				keys = append(keys, key)
			}
			i += n
		case c == '\r' || c == '\n':
			keys = append(keys, "enter")
			i++
		case c == '\t':
			keys = append(keys, "tab")
			i++
		case c == 0x7f || c == 0x08:
			keys = append(keys, "backspace")
			i++
		case c == 0x03:
			keys = append(keys, "ctrl-c")
			i++
		case c >= 32 && c < 127:
			keys = append(keys, string(c))
			i++
		default:
			i++ // other control bytes and non-ASCII input are ignored
		}
	}
	return keys
}

// parseEscape decodes the sequence starting at b[0] == ESC. It returns the key
// ("" when the sequence is to be ignored) and the number of bytes consumed.
func parseEscape(b []byte) (key string, n int) {
	if len(b) == 1 {
		return "esc", 1 // the Escape key on its own
	}
	switch b[1] {
	case '[':
		return parseCSI(b)
	case 'O':
		if len(b) < 3 {
			return "", len(b) // incomplete: drop it
		}
		return cursorKey(b[2]), 3
	default:
		return "esc", 1 // Escape followed by something else: that is parsed on its own
	}
}

// parseCSI decodes ESC [ <parameters> <intermediates> <final>.
func parseCSI(b []byte) (key string, n int) {
	j := 2
	for j < len(b) && b[j] >= 0x30 && b[j] <= 0x3f { // parameter bytes
		j++
	}
	paramEnd := j
	for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f { // intermediate bytes
		j++
	}
	if j >= len(b) {
		return "", len(b) // incomplete: drop it
	}
	final := b[j]
	if final < 0x40 || final > 0x7e {
		return "", j // not a valid CSI: skip what we scanned, parse the rest normally
	}
	if final == '~' {
		return tildeKey(string(b[2:paramEnd])), j + 1
	}
	return cursorKey(final), j + 1
}

// cursorKey maps the final byte of an arrow/home/end sequence.
func cursorKey(final byte) string {
	switch final {
	case 'A':
		return "up"
	case 'B':
		return "down"
	case 'H':
		return "home"
	case 'F':
		return "end"
	}
	return ""
}

// tildeKey maps the numeric parameter of an ESC [ n ~ sequence; modifiers after
// a ';' are ignored.
func tildeKey(params string) string {
	for i := 0; i < len(params); i++ {
		if params[i] == ';' {
			params = params[:i]
			break
		}
	}
	switch params {
	case "1", "7":
		return "home"
	case "4", "8":
		return "end"
	case "5":
		return "pgup"
	case "6":
		return "pgdn"
	}
	return ""
}
