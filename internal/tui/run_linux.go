//go:build linux

// The run loop of the TUI: owns the terminal, feeds the model from the journal
// (replay + live) and redraws on every change.
package tui

import (
	"context"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
)

// Run starts the interactive boot monitor. stdin and stdout must be terminals;
// the caller is expected to check with IsTerminalable and report
// ErrNotATTY nicely. The collector feeds the replay + live stream; opt controls
// which boot is watched.
func Run(ctx context.Context, in *os.File, out *os.File, c *collector.Collector, opt collector.Options, initialFilter string) error {
	w, h, err := terminalSize(in.Fd())
	if err != nil {
		return err
	}

	raw, err := enterRaw(in.Fd())
	if err != nil {
		return err
	}
	defer raw.restore()

	m := New()
	m.Resize(w, h)
	if initialFilter != "" {
		if err := m.SetFilter(initialFilter); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Terminal control sequences: alternate screen, hide cursor, clear.
	io.WriteString(out, "\x1b[?1049h\x1b[?25l\x1b[2J")
	defer io.WriteString(out, "\x1b[?25h\x1b[?1049l") // restore screen and cursor

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			if nw, nh, err := terminalSize(in.Fd()); err == nil {
				m.Resize(nw, nh)
			}
			m.requestRedraw()
		}
	}()

	keys := make(chan string, 32)
	go readKeys(in, keys)

	wake := make(chan struct{}, 1)
	m.waker = func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}

	// Journal feed: replay the boot, then follow live.
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- c.Watch(ctx, opt, func(ev event.Event) error {
			m.mu.Lock()
			m.Append(interpret.Interpret(ev))
			m.mu.Unlock()
			m.requestRedraw()
			return nil
		})
	}()

	draw := func() {
		w, h, _ := terminalSize(in.Fd())
		f := Render(m, w, h)
		var b strings.Builder
		b.WriteString("\x1b[H")
		for _, l := range f.Lines {
			if l.Reverse {
				b.WriteString("\x1b[7m")
			}
			b.WriteString(l.Text)
			// Pad so remnants of previous frames are erased even without colors.
			for i := len(l.Text); i < w; i++ {
				b.WriteByte(' ')
			}
			if l.Reverse {
				b.WriteString("\x1b[27m")
			}
			b.WriteString("\x1b[K\r\n")
		}
		io.WriteString(out, b.String())
	}

	draw()
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-watchErr:
			if err != nil {
				return err
			}
			return nil
		case key, ok := <-keys:
			if !ok {
				// stdin closed (EOF): nothing more to read, leave cleanly.
				return nil
			}
			m.mu.Lock()
			m.Key(key)
			quitting := m.Quitting()
			m.mu.Unlock()
			if quitting {
				return nil
			}
			draw()
		case <-wake:
			draw()
		}
	}
}

// readKeys turns terminal input into key names for the model. Sequences are
// read in one go (terminals deliver escape sequences as a single burst).
func readKeys(in io.Reader, out chan<- string) {
	buf := make([]byte, 16)
	for {
		n, err := in.Read(buf)
		if err != nil {
			close(out)
			return
		}
		parseKeys(buf[:n], out)
	}
}

func parseKeys(b []byte, out chan<- string) {
	for i := 0; i < len(b); {
		switch {
		case b[i] == 0x1b && i+2 < len(b) && b[i+1] == '[':
			switch b[i+2] {
			case 'A':
				out <- "up"
			case 'B':
				out <- "down"
			case 'H':
				out <- "home"
			case 'F':
				out <- "end"
			case '5':
				if i+3 < len(b) && b[i+3] == '~' {
					out <- "pgup"
					i++
				} else {
					out <- "esc"
				}
			case '6':
				if i+3 < len(b) && b[i+3] == '~' {
					out <- "pgdn"
					i++
				} else {
					out <- "esc"
				}
			default:
				out <- "esc"
			}
			i += 3
		case b[i] == 0x1b:
			out <- "esc"
			i++
		case b[i] == '\r' || b[i] == '\n':
			out <- "enter"
			i++
		case b[i] == '\t':
			out <- "tab"
			i++
		case b[i] == 0x7f || b[i] == 0x08:
			out <- "backspace"
			i++
		case b[i] == 0x03:
			out <- "ctrl-c"
			i++
		case b[i] >= 32 && b[i] < 127:
			out <- string(b[i])
			i++
		default:
			i++ // ignore anything unrecognized
		}
	}
}

// requestRedraw schedules a redraw from the main loop.
func (m *Model) requestRedraw() {
	if m.waker != nil {
		m.waker()
	}
}
