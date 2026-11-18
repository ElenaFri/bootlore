//go:build linux

// Terminal glue for the TUI: raw mode, key parsing and winsize, using only the
// standard library (syscall ioctls). Linux only, like BootLore itself.
package tui

import (
	"errors"
	"os"
	"syscall"
	"unsafe"
)

// ErrNotATTY is returned when the TUI is started without a terminal.
var ErrNotATTY = errors.New("the TUI needs an interactive terminal (stdin and stdout must be ttys)")

func ioctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// isTerminal reports whether fd refers to a terminal.
func isTerminal(fd uintptr) bool {
	var termios syscall.Termios
	return ioctl(fd, syscall.TCGETS, unsafe.Pointer(&termios)) == nil
}

// IsTerminalable reports whether in and out are both terminals.
func IsTerminalable(in *os.File, out *os.File) bool {
	return in != nil && out != nil && isTerminal(in.Fd()) && isTerminal(out.Fd())
}

// rawState remembers the terminal settings to restore on exit.
type rawState struct {
	fd    uintptr
	saved syscall.Termios
}

// enterRaw disables echo and line buffering (cfmakeraw-like) on fd.
func enterRaw(fd uintptr) (*rawState, error) {
	var t syscall.Termios
	if err := ioctl(fd, syscall.TCGETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	saved := t

	t.Iflag &^= syscall.IGNBRK | syscall.BRKINT | syscall.PARMRK | syscall.ISTRIP |
		syscall.INLCR | syscall.IGNCR | syscall.ICRNL | syscall.IXON
	t.Oflag &^= syscall.OPOST
	t.Lflag &^= syscall.ECHO | syscall.ECHONL | syscall.ICANON | syscall.ISIG | syscall.IEXTEN
	t.Cflag &^= syscall.CSIZE | syscall.PARENB
	t.Cflag |= syscall.CS8
	t.Cc[syscall.VMIN] = 1
	t.Cc[syscall.VTIME] = 0

	if err := ioctl(fd, syscall.TCSETS, unsafe.Pointer(&t)); err != nil {
		return nil, err
	}
	return &rawState{fd: fd, saved: saved}, nil
}

// restore puts the terminal back to its previous settings.
func (s *rawState) restore() {
	if s != nil {
		_ = ioctl(s.fd, syscall.TCSETS, unsafe.Pointer(&s.saved))
	}
}

type winsize struct {
	Row, Col, X, Y uint16
}

// terminalSize returns the current width and height of fd.
func terminalSize(fd uintptr) (int, int, error) {
	var ws winsize
	if err := ioctl(fd, syscall.TIOCGWINSZ, unsafe.Pointer(&ws)); err != nil {
		return 0, 0, err
	}
	return int(ws.Col), int(ws.Row), nil
}
