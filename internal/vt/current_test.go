//go:build linux

package vt

import (
	"strconv"
	"testing"
)

// --- RequireCurrent: the check for a service that was handed a terminal ---

// The service (systemd TTYPath=/dev/ttyN) hands bootlore the terminal on fd:
// RequireCurrent verifies that fd really is the requested virtual terminal,
// that nobody else holds it, and that it is not the active console.
func TestRequireCurrentAcceptsMatchingFreeTerminal(t *testing.T) {
	p := &Probe{
		VTBusy:   func(n int) bool { return false },
		VTActive: func() (int, bool) { return 1, true },
	}
	if err := p.RequireCurrent(rdevTTY(9), 9); err != nil {
		t.Errorf("RequireCurrent(tty9, 9) = %v, want nil", err)
	}
}

func TestRequireCurrentRejectsWrongTerminal(t *testing.T) {
	p := &Probe{
		VTBusy:   func(n int) bool { return false },
		VTActive: func() (int, bool) { return 1, true },
	}
	err := p.RequireCurrent(rdevTTY(7), 9)
	if err == nil {
		t.Fatal("RequireCurrent must refuse a terminal other than the requested one")
	}
	if want := "input is tty7, not tty9"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestRequireCurrentRejectsNonVTInput(t *testing.T) {
	p := &Probe{
		VTBusy:   func(n int) bool { return false },
		VTActive: func() (int, bool) { return 1, true },
	}
	err := p.RequireCurrent(rdevPTY, 9)
	if err == nil {
		t.Fatal("RequireCurrent must refuse a pty (not a virtual terminal)")
	}
	if want := "input is not the requested virtual terminal 9"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// Our own controlling terminal must not count as busy: when systemd hands the
// service ttyN, the service itself becomes its controlling process and the
// /proc scan would see it. The holder we are is the holder we want.
func TestRequireCurrentExcludesOwnControllingTerminal(t *testing.T) {
	calls := 0
	p := &Probe{
		VTBusy: func(n int) bool {
			calls++
			return true // someone in /proc holds tty9 — namely, us
		},
		VTActive: func() (int, bool) { return 1, true },
		selfTTY:  ttyNr(9),
	}
	if err := p.RequireCurrent(rdevTTY(9), 9); err != nil {
		t.Errorf("RequireCurrent must tolerate holding the terminal itself: %v", err)
	}
	if calls != 1 {
		t.Errorf("VTBusy called %d times, want 1", calls)
	}
}

// A terminal held by someone else — a getty, Xorg — is refused, and the
// message says which terminal is occupied even when we also hold it.
func TestRequireCurrentRejectsHeldByOthers(t *testing.T) {
	p := &Probe{
		VTBusy: func(n int) bool { return true },
		// No selfTTY: the holder is somebody else entirely.
		VTActive: func() (int, bool) { return 1, true },
	}
	err := p.RequireCurrent(rdevTTY(9), 9)
	if err == nil {
		t.Fatal("RequireCurrent must refuse a terminal held by another process")
	}
	if !errorIs(err, ErrBusy) {
		t.Errorf("error = %v, want ErrBusy inside", err)
	}
}

func TestRequireCurrentRejectsActiveConsole(t *testing.T) {
	p := &Probe{
		VTBusy:   func(n int) bool { return false },
		VTActive: func() (int, bool) { return 9, true },
		selfTTY:  ttyNr(9),
	}
	err := p.RequireCurrent(rdevTTY(9), 9)
	if err == nil {
		t.Fatal("RequireCurrent must refuse the active console")
	}
	if !errorIs(err, ErrActive) {
		t.Errorf("error = %v, want ErrActive inside", err)
	}
}

func TestRequireCurrentValidatesNumber(t *testing.T) {
	p := &Probe{}
	for _, bad := range []int{0, -1, 64} {
		if err := p.RequireCurrent(rdevTTY(uint64(bad)), bad); err == nil {
			t.Errorf("RequireCurrent(%d) should fail: VT numbers are 1..63", bad)
		}
	}
}

// --- pure helpers ---

func TestIsVT(t *testing.T) {
	if n, ok := IsVT(rdevTTY(12)); !ok || n != 12 {
		t.Errorf("IsVT(tty12) = (%d, %v), want (12, true)", n, ok)
	}
	if _, ok := IsVT(rdevPTY); ok {
		t.Error("IsVT(pty) should be false: a pty is not a virtual terminal")
	}
	if _, ok := IsVT(rdevTTY(0)); ok {
		t.Error("IsVT(tty0) should be false: tty0 is the console, not a usable terminal")
	}
}

// rdevTTY builds a device number for /dev/ttyN: char major 4, minor N.
func rdevTTY(n uint64) uint64 { return 4<<8 | n }

// rdevPTY builds a device number for a pty slave (char major 136).
var rdevPTY uint64 = 136<<8 | 7

// ttyNr renders the /proc/PID/stat tty_nr encoding of ttyN (major<<8|minor).
func ttyNr(n int) string { return strconv.Itoa(4<<8 + n) }
