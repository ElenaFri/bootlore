//go:build linux

// The run loop of the TUI: owns the terminal, feeds the model from the journal
// (replay + live) and redraws on every change.
package tui

import (
	"context"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
)

// Run starts the interactive boot monitor. in and out must be terminals; the
// caller checks with IsTerminalable and reports ErrNotATTY nicely. The
// collector feeds the replay-then-live stream and opt selects the boot.
// initialFilter and initialView set the starting category filter and list
// representation (LOG or HUMAN); both stay switchable in-session.
//
// Run returns nil when the user quits (or ctx is canceled, or the terminal
// goes away) and an error when the session could not start: an invalid filter,
// a file that is not a terminal, or a journal backend that failed before
// delivering anything. Once events are on screen a journal problem is shown in
// the status bar instead, so what was collected stays browsable.
func Run(ctx context.Context, in, out *os.File, c *collector.Collector, opt collector.Options, initialFilter string, initialView View) error {
	m := New()
	m.SetView(initialView)

	// Validate everything that can be refused before touching the terminal.
	if initialFilter != "" {
		if err := m.SetFilter(initialFilter); err != nil {
			return err
		}
	}
	w, h, err := terminalSize(in.Fd())
	if err != nil {
		return err
	}
	m.Resize(w, h)

	raw, err := enterRaw(in.Fd())
	if err != nil {
		return err
	}
	defer raw.restore()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	io.WriteString(out, enterScreen)
	defer io.WriteString(out, leaveScreen)

	// The redraw request must be wired before any goroutine can issue one.
	wake := make(chan struct{}, 1)
	m.waker = func() {
		select {
		case wake <- struct{}{}:
		default: // a redraw is already pending
		}
	}

	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for {
			select {
			case <-winch:
				m.requestRedraw()
			case <-ctx.Done():
				return
			}
		}
	}()

	keys := make(chan string, 32)
	go readKeys(in, keys)

	// Journal feed: replay the boot, then follow it live.
	watchErr := make(chan error, 1)
	go func() {
		watchErr <- c.Watch(ctx, opt, func(ev event.Event) error {
			e := interpret.Interpret(ev)
			m.mu.Lock()
			m.Append(e)
			m.mu.Unlock()
			m.requestRedraw()
			return nil
		})
	}()

	// draw re-reads the terminal size, so a resize needs no separate handling:
	// the size recorded in the model always comes from this one place.
	draw := func() {
		m.mu.Lock()
		if nw, nh, err := terminalSize(in.Fd()); err == nil {
			m.Resize(nw, nh)
		}
		w, h := m.Size()
		f := Render(m, w, h)
		m.mu.Unlock()
		io.WriteString(out, encodeFrame(f))
	}

	draw()
	for {
		select {
		case <-ctx.Done():
			return nil

		case err := <-watchErr:
			m.mu.Lock()
			if err != nil && m.AllCount() == 0 {
				m.mu.Unlock()
				return err
			}
			if err != nil {
				m.SetStatus("journal error: " + err.Error())
			} else {
				m.SetStatus("journal stream ended (q to quit)")
			}
			m.mu.Unlock()
			draw()

		case key, ok := <-keys:
			if !ok {
				return nil // the terminal closed: nothing more to read
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
