//go:build linux

// Package ptytest gives tests a real pseudo-terminal pair, using only the
// standard library, so terminal code (raw mode, window size, the TUI run loop)
// can be exercised for real instead of being skipped. It is imported only by
// tests.
package ptytest

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Pty is a pseudo-terminal pair. The code under test uses Slave as its
// terminal; the test plays the user through Master: bytes written to Master are
// typed input, bytes the program prints can be read back with Output/WaitFor.
type Pty struct {
	Master *os.File
	Slave  *os.File

	mu  sync.Mutex
	buf bytes.Buffer
}

// New opens a pseudo-terminal with the given size and registers its cleanup.
// The test is skipped when the system offers no /dev/ptmx (some sandboxes).
func New(t *testing.T, cols, rows int) *Pty {
	t.Helper()

	master, err := openPollable("/dev/ptmx")
	if err != nil {
		t.Skipf("no pseudo-terminal available: %v", err)
	}

	var unlock int32
	if err := ioctl(master.Fd(), syscall.TIOCSPTLCK, unsafe.Pointer(&unlock)); err != nil {
		master.Close()
		t.Fatalf("unlocking the pty: %v", err)
	}
	var n uint32
	if err := ioctl(master.Fd(), syscall.TIOCGPTN, unsafe.Pointer(&n)); err != nil {
		master.Close()
		t.Fatalf("reading the pty number: %v", err)
	}
	slave, err := openPollable(fmt.Sprintf("/dev/pts/%d", n))
	if err != nil {
		master.Close()
		t.Fatalf("opening the pty slave: %v", err)
	}

	p := &Pty{Master: master, Slave: slave}
	p.Resize(t, cols, rows)
	t.Cleanup(p.Close)
	go p.pump()
	return p
}

// Close releases both ends. It is safe to call more than once.
func (p *Pty) Close() {
	p.Slave.Close()
	p.Master.Close()
}

// openPollable opens a terminal device in non-blocking mode so that Go
// registers it with its poller. A plain blocking file would make Close unable
// to interrupt a read in progress: the device would then stay open, and the
// other end of the pty would never see the hang-up.
func openPollable(path string) (*os.File, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(fd), path), nil
}

type winsize struct{ Row, Col, X, Y uint16 }

// Resize sets the terminal size, as a terminal emulator would.
func (p *Pty) Resize(t *testing.T, cols, rows int) {
	t.Helper()
	ws := winsize{Row: uint16(rows), Col: uint16(cols)}
	if err := ioctl(p.Master.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		t.Fatalf("setting the pty size: %v", err)
	}
}

// Type sends input as if the user had typed it.
func (p *Pty) Type(t *testing.T, s string) {
	t.Helper()
	if _, err := p.Master.WriteString(s); err != nil {
		t.Fatalf("typing %q: %v", s, err)
	}
}

// Output returns everything the program has printed so far.
func (p *Pty) Output() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.String()
}

// Mark returns the current length of the output, to wait for what follows it.
func (p *Pty) Mark() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.buf.Len()
}

// WaitFor blocks until substr appears in the output printed after mark, and
// fails the test if it does not within the timeout.
func (p *Pty) WaitFor(t *testing.T, mark int, substr string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if out := p.Output(); len(out) >= mark && strings.Contains(out[mark:], substr) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q; output since the mark:\n%q", substr, p.Output()[min(mark, len(p.Output())):])
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// pump copies the program's output into the buffer until the pty closes.
func (p *Pty) pump() {
	chunk := make([]byte, 4096)
	for {
		n, err := p.Master.Read(chunk)
		if n > 0 {
			p.mu.Lock()
			p.buf.Write(chunk[:n])
			p.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); errno != 0 {
		return errno
	}
	return nil
}
