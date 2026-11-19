//go:build linux

package tui

import (
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ElenaFri/bootlore/internal/ptytest"
)

func regularFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "plain")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

func termios(t *testing.T, f *os.File) syscall.Termios {
	t.Helper()
	var tm syscall.Termios
	if err := ioctl(f.Fd(), syscall.TCGETS, unsafe.Pointer(&tm)); err != nil {
		t.Fatalf("reading termios: %v", err)
	}
	return tm
}

func TestIsTerminal(t *testing.T) {
	p := ptytest.New(t, 80, 24)
	if !isTerminal(p.Slave.Fd()) {
		t.Error("a pty slave is a terminal")
	}
	if isTerminal(regularFile(t).Fd()) {
		t.Error("a regular file is not a terminal")
	}
}

func TestIsTerminalable(t *testing.T) {
	p := ptytest.New(t, 80, 24)
	plain := regularFile(t)

	if !IsTerminalable(p.Slave, p.Slave) {
		t.Error("two terminals should do")
	}
	if IsTerminalable(p.Slave, plain) {
		t.Error("a redirected stdout is not enough")
	}
	if IsTerminalable(plain, p.Slave) {
		t.Error("a redirected stdin is not enough")
	}
	if IsTerminalable(nil, p.Slave) || IsTerminalable(p.Slave, nil) {
		t.Error("a missing file is not a terminal")
	}
}

func TestTerminalSize(t *testing.T) {
	p := ptytest.New(t, 100, 30)
	w, h, err := terminalSize(p.Slave.Fd())
	if err != nil {
		t.Fatalf("terminalSize: %v", err)
	}
	if w != 100 || h != 30 {
		t.Errorf("terminalSize = %dx%d, want 100x30", w, h)
	}

	p.Resize(t, 132, 43)
	if w, h, _ := terminalSize(p.Slave.Fd()); w != 132 || h != 43 {
		t.Errorf("after a resize: %dx%d, want 132x43", w, h)
	}

	if _, _, err := terminalSize(regularFile(t).Fd()); err == nil {
		t.Error("a regular file has no size: expected an error")
	}
}

// Raw mode must stop the line discipline from echoing and buffering input, and
// restoring must put the original settings back exactly.
func TestEnterRawAndRestore(t *testing.T) {
	p := ptytest.New(t, 80, 24)
	before := termios(t, p.Slave)
	if before.Lflag&syscall.ECHO == 0 || before.Lflag&syscall.ICANON == 0 {
		t.Fatal("precondition: a fresh pty echoes and buffers lines")
	}

	raw, err := enterRaw(p.Slave.Fd())
	if err != nil {
		t.Fatalf("enterRaw: %v", err)
	}
	during := termios(t, p.Slave)
	if during.Lflag&(syscall.ECHO|syscall.ICANON|syscall.ISIG) != 0 {
		t.Errorf("raw mode left echo/canonical/signals on: Lflag = %#x", during.Lflag)
	}
	if during.Cc[syscall.VMIN] != 1 || during.Cc[syscall.VTIME] != 0 {
		t.Errorf("raw mode should block for one byte: VMIN=%d VTIME=%d", during.Cc[syscall.VMIN], during.Cc[syscall.VTIME])
	}

	raw.restore()
	after := termios(t, p.Slave)
	if after != before {
		t.Errorf("restore did not bring back the original settings:\n before = %+v\n after  = %+v", before, after)
	}
}

func TestEnterRawOnANonTerminalFails(t *testing.T) {
	if _, err := enterRaw(regularFile(t).Fd()); err == nil {
		t.Error("enterRaw on a regular file should fail")
	}
}

func TestRestoreNilIsHarmless(t *testing.T) {
	var raw *rawState
	raw.restore() // must not panic
}
