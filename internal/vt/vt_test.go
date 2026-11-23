//go:build linux

package vt

import (
	"errors"
	"testing"
)

func TestPickReturnsRequestedFreeVT(t *testing.T) {
	p := NewProbe()
	p.VTExists = func(n int) bool { return n >= 1 && n <= 63 }
	p.VTBusy = func(n int) bool { return n <= 6 } // gettys on tty1..tty6
	p.VTActive = func() (int, bool) { return 1, true }

	n, err := p.Pick(9)
	if err != nil {
		t.Fatalf("Pick(9) error: %v", err)
	}
	if n != 9 {
		t.Errorf("Pick(9) = %d, want 9 (unchanged when free)", n)
	}
}

func TestPickRejectsNonexistentVT(t *testing.T) {
	p := NewProbe()
	p.VTExists = func(n int) bool { return n <= 6 }
	p.VTBusy = func(n int) bool { return false }
	p.VTActive = func() (int, bool) { return 1, true }

	_, err := p.Pick(9)
	if err == nil {
		t.Fatal("Pick(9) should fail when the VT does not exist")
	}
	if want := "terminal 9 does not exist"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestPickRejectsBusyVT(t *testing.T) {
	p := NewProbe()
	p.VTExists = func(n int) bool { return true }
	p.VTBusy = func(n int) bool { return n == 7 } // a getty already runs there
	p.VTActive = func() (int, bool) { return 1, true }

	_, err := p.Pick(7)
	if err == nil {
		t.Fatal("Pick(7) should fail when the VT is busy")
	}
	if want := "terminal 7 is already in use"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// BootLore must never take control of the console the user is looking at: the
// active VT is off-limits even when it exists and is otherwise free.
func TestPickRejectsActiveVT(t *testing.T) {
	p := NewProbe()
	p.VTExists = func(n int) bool { return true }
	p.VTBusy = func(n int) bool { return false }
	p.VTActive = func() (int, bool) { return 9, true }

	_, err := p.Pick(9)
	if err == nil {
		t.Fatal("Pick(9) should fail when 9 is the active console")
	}
	if want := "terminal 9 is the active console; BootLore never takes control of it"; err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// When the active VT cannot be determined (no VT subsystem, container, serial
// console), the active check is skipped rather than failing every VT.
func TestPickToleratesUnknownActiveVT(t *testing.T) {
	p := NewProbe()
	p.VTExists = func(n int) bool { return true }
	p.VTBusy = func(n int) bool { return false }
	p.VTActive = func() (int, bool) { return 0, false }

	n, err := p.Pick(9)
	if err != nil {
		t.Fatalf("Pick(9) should tolerate an unknown active console: %v", err)
	}
	if n != 9 {
		t.Errorf("Pick(9) = %d, want 9", n)
	}
}

func TestPickValidatesNumber(t *testing.T) {
	p := NewProbe()
	for _, bad := range []int{0, -1, 64, 1000} {
		if _, err := p.Pick(bad); err == nil {
			t.Errorf("Pick(%d) should fail: VT numbers are 1..63", bad)
		}
	}
}

// errorIs is errors.Is spelled locally for use in the table below.
func errorIs(err, target error) bool { return errors.Is(err, target) }

// IsNoSuch reports whether the error means "this terminal does not exist",
// as opposed to occupied or active.
func TestIsNoSuch(t *testing.T) {
	if !IsNoSuch(ErrNotExist) {
		t.Error("ErrNotExist should satisfy IsNoSuch")
	}
	if IsNoSuch(ErrBusy) {
		t.Error("ErrBusy should not satisfy IsNoSuch")
	}
}

// The sentinel travels through the reason error that Pick returns, so
// callers can classify refusals with errors.Is while users read a clean
// message.
func TestPickReasonStillCarriesSentinels(t *testing.T) {
	cases := []struct {
		name   string
		probe  *Probe
		sentin error
	}{
		{"not exist", &Probe{VTExists: func(int) bool { return false }}, ErrNotExist},
		{"busy", &Probe{VTExists: func(int) bool { return true }, VTBusy: func(int) bool { return true }}, ErrBusy},
		{"active", &Probe{
			VTExists: func(int) bool { return true },
			VTBusy:   func(int) bool { return false },
			VTActive: func() (int, bool) { return 9, true },
		}, ErrActive},
	}
	for _, tc := range cases {
		_, err := tc.probe.Pick(9)
		if err == nil {
			t.Errorf("%s: Pick should fail", tc.name)
			continue
		}
		if !errorIs(err, tc.sentin) {
			t.Errorf("%s: errors.Is should find %v in %v", tc.name, tc.sentin, err)
		}
	}
}
