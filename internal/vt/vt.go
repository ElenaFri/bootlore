//go:build linux

// Package vt implements the virtual-terminal half of BootLore's optional
// automatic startup mode (architecture.md §6): the unit runs on a spare VT,
// never the one the user is looking at, and exits gracefully when the
// requested terminal is unavailable or occupied.
//
// The decision logic (Probe.Pick) is pure and probe-injected; the Linux probes
// need no privileges and no ioctls: existence is an open of /dev/ttyN,
// occupancy is the controlling terminal recorded in /proc (getty, Xorg...),
// and the active console is /sys/class/tty/tty0/active. Tests replace the
// probes, so the rules are unit-testable without any real terminal.
package vt

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Sentinel errors for unusable terminals. They are distinct so callers can
// report precisely why the requested terminal was refused.
var (
	// ErrNotExist means the kernel has no such virtual terminal.
	ErrNotExist = errors.New("virtual terminal does not exist")
	// ErrBusy means another process already uses the terminal as its
	// controlling terminal (a getty, Xorg, ...).
	ErrBusy = errors.New("virtual terminal is already in use")
	// ErrActive means the terminal is the console the user is looking at,
	// which BootLore must never take control of.
	ErrActive = errors.New("virtual terminal is the active console")
)

// IsNoSuch reports whether err means "this terminal does not exist", as
// opposed to occupied or active.
func IsNoSuch(err error) bool { return errors.Is(err, ErrNotExist) }

// reasonError says exactly what is wrong with one terminal, while wrapping
// the matching sentinel so errors.Is keeps working — without repeating the
// sentinel's text at the end of the message.
type reasonError struct {
	msg   string
	sentn error
}

func (e *reasonError) Error() string { return e.msg }
func (e *reasonError) Unwrap() error { return e.sentn }

// Probe answers the three questions of terminal selection. Every field is a
// function so tests can supply canned answers; NewProbe installs the real
// Linux implementations.
type Probe struct {
	// VTExists reports whether virtual terminal n exists.
	VTExists func(n int) bool
	// VTBusy reports whether another process holds virtual terminal n.
	VTBusy func(n int) bool
	// VTActive returns the currently active virtual terminal, or false when
	// it cannot be determined (no VT subsystem, container, serial console).
	VTActive func() (int, bool)
	// selfTTY is the tty_nr of this process's own controlling terminal
	// ("" when it has none). RequireCurrent excludes it from the busy
	// scan: a service legitimately holds the terminal it was given.
	selfTTY string
}

// NewProbe returns a Probe wired to the real Linux virtual terminals. The
// process's own controlling terminal is remembered so RequireCurrent does
// not mistake the service itself for an intruding holder. On a system without
// VTs (container, serial console) each probe degrades to "unknown" instead
// of failing.
func NewProbe() *Probe {
	self := ""
	if data, err := os.ReadFile("/proc/self/stat"); err == nil {
		self = controllingTTY(string(data))
	}
	return &Probe{
		VTExists: func(n int) bool { return existsAt("/dev", n) },
		VTBusy:   func(n int) bool { return busyFrom("/proc", n) },
		VTActive: func() (int, bool) { return activeFrom("/sys/class/tty/tty0/active") },
		selfTTY:  self,
	}
}

// IsVT decodes a device number (as reported by fstat on a terminal file)
// and returns the virtual terminal it names, when it names one: virtual
// terminals are character devices with major 4 and a minor of 1..63. tty0
// (minor 0) is the console itself, not a usable terminal.
func IsVT(rdev uint64) (int, bool) {
	major := (rdev >> 8) & 0xfff
	minor := rdev & 0xff
	if major != 4 || minor < 1 || minor > 63 {
		return 0, false
	}
	return int(minor), true
}

// RequireCurrent validates the terminal a service handed this process on the
// given device number: it must be the requested virtual terminal n (exactly
// n — a unit configured with TTYPath=tty9 must not silently end up on tty8),
// it must not be held by another process and it must not be the active
// console. This process's own controlling terminal does not count as busy:
// when systemd hands the service ttyN, the service itself becomes its
// holding process, which is exactly the intended situation. Every refusal
// wraps the same sentinels as Pick.
func (p *Probe) RequireCurrent(rdev uint64, n int) error {
	if n < 1 || n > 63 {
		return fmt.Errorf("invalid terminal number %d (want 1..63)", n)
	}
	actual, ok := IsVT(rdev)
	if !ok {
		return fmt.Errorf("input is not the requested virtual terminal %d", n)
	}
	if actual != n {
		return fmt.Errorf("input is tty%d, not tty%d", actual, n)
	}
	busy := p.VTBusy != nil && p.VTBusy(n)
	if busy && strconv.Itoa(4<<8+n) != p.selfTTY {
		return &reasonError{fmt.Sprintf("terminal %d is already in use", n), ErrBusy}
	}
	if active, ok := p.VTActive(); ok && active == n {
		return &reasonError{fmt.Sprintf("terminal %d is the active console; BootLore never takes control of it", n), ErrActive}
	}
	return nil
}

// Pick validates that virtual terminal n can host the BootLore TUI: it must
// be a legal VT number (1..63), exist, be free and not be the active console.
// The requested number is never changed: silently moving to another tty would
// take the user by surprise (architecture.md §6).
func (p *Probe) Pick(n int) (int, error) {
	if n < 1 || n > 63 {
		return 0, fmt.Errorf("invalid terminal number %d (want 1..63)", n)
	}
	if !p.VTExists(n) {
		return 0, &reasonError{fmt.Sprintf("terminal %d does not exist", n), ErrNotExist}
	}
	if p.VTBusy(n) {
		return 0, &reasonError{fmt.Sprintf("terminal %d is already in use", n), ErrBusy}
	}
	if active, ok := p.VTActive(); ok && active == n {
		return 0, &reasonError{fmt.Sprintf("terminal %d is the active console; BootLore never takes control of it", n), ErrActive}
	}
	return n, nil
}

// existsAt reports whether virtual terminal n can be opened under the given
// device directory. The check opens without taking control (O_NOCTTY) and
// closes immediately; it disturbs nothing.
func existsAt(devDir string, n int) bool {
	f, err := os.OpenFile(filepath.Join(devDir, fmt.Sprintf("tty%d", n)), os.O_WRONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// busyFrom reports whether any process under the given /proc tree holds
// virtual terminal n as its controlling terminal. /proc/PID/stat reports
// that terminal as MAJOR<<8 | MINOR; virtual terminals are character
// major 4, so ttyN is 4<<8 + N. The scan is unprivileged and read-only.
func busyFrom(procDir string, n int) bool {
	want := strconv.Itoa(4<<8 + n)
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(procDir, e.Name(), "stat"))
		if err != nil {
			continue // process vanished or not ours to see: fine
		}
		if controllingTTY(string(data)) == want {
			return true
		}
	}
	return false
}

// controllingTTY extracts the tty_nr field of a /proc/PID/stat line, i.e. the
// 7th field. comm (field 2) may contain spaces and parentheses, so the line
// is split after its closing parenthesis, leaving tty_nr the 5th remaining
// field.
func controllingTTY(stat string) string {
	i := strings.LastIndex(stat, ")")
	if i < 0 {
		return ""
	}
	fields := strings.Fields(stat[i+1:])
	if len(fields) < 5 {
		return ""
	}
	return fields[4]
}

// activeFrom returns the currently active virtual terminal named by the
// kernel file at path (\n terminated, e.g. "tty7\n"), or (0, false) when it
// cannot be determined (no VT subsystem).
func activeFrom(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	name := strings.TrimSpace(string(data))
	if !strings.HasPrefix(name, "tty") {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimPrefix(name, "tty"))
	if err != nil {
		return 0, false
	}
	return n, true
}
