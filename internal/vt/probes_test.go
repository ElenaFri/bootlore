//go:build linux

package vt

import (
	"os"
	"path/filepath"
	"testing"
)

// --- the real probes, against recorded fixtures (no live system needed) ---

func TestControllingTTSkipsCommWithParensAndSpaces(t *testing.T) {
	// comm (field 2) may contain anything but ')' — here parentheses and
	// spaces — so parsing must anchor after the *last* ')'.
	line := "17148 ((sd-pam) helper) S 1200 17148 17148 0 -1 4194560"
	if got := controllingTTY(line); got != "0" {
		t.Errorf("controllingTTY = %q, want 0", got)
	}
	if got := controllingTTY("no parens at all"); got != "" {
		t.Errorf("controllingTTY garbage = %q, want empty", got)
	}
	// A line that ends right after the parentheses has no tty_nr field.
	if got := controllingTTY("1283 (agetty)"); got != "" {
		t.Errorf("controllingTTY short line = %q, want empty", got)
	}
}

func TestBusyDetectsHelderOfRequestedVT(t *testing.T) {
	// agetty on tty1 (major 4, minor 1 -> 1025) ; nothing on tty8 (1032)
	dir := fakeProc(t,
		"1283 (agetty) S 1 1283 1283 1025 -1",
		"999 (misch) S 1 999 999 1031 -1", // Xorg-like holder of tty7
	)
	if !busyFrom(dir, 1) {
		t.Error("tty1 is held by agetty, busyFrom must report it busy")
	}
	if !busyFrom(dir, 7) {
		t.Error("tty7 is held, busyFrom must report it busy")
	}
	if busyFrom(dir, 8) {
		t.Error("tty8 has no holder, busyFrom must report it free")
	}
}

func TestBusyToleratesVanishedProcesses(t *testing.T) {
	dir := t.TempDir()
	// A stat file that is not a process dir is skipped without error.
	pdir := filepath.Join(dir, "1")
	if err := os.Mkdir(pdir, 0o755); err != nil {
		t.Fatalf("making fake process dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(pdir, "stat"), nil, 0o644); err != nil {
		t.Fatalf("writing empty stat: %v", err)
	}
	if busyFrom(dir, 1) {
		t.Error("no readable process holds tty1 here")
	}
	// A nonexistent /proc tree yields false, not a crash.
	if busyFrom(filepath.Join(dir, "gone"), 1) {
		t.Error("a missing /proc must read as free")
	}
	// Non-directory entries are ignored.
	if err := os.WriteFile(filepath.Join(dir, "loadavg"), nil, 0o644); err != nil {
		t.Fatalf("writing non-dir entry: %v", err)
	}
	if busyFrom(dir, 1) {
		t.Error("non-directory entries must be skipped")
	}
}

func TestActiveParsesKernelFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active")
	if err := os.WriteFile(path, []byte("tty7\n"), 0o644); err != nil {
		t.Fatalf("writing active file: %v", err)
	}
	n, ok := activeFrom(path)
	if !ok || n != 7 {
		t.Errorf("activeFrom = (%d, %v), want (7, true)", n, ok)
	}
}

func TestActiveUnknownWithoutVTSubsystem(t *testing.T) {
	n, ok := activeFrom(filepath.Join(t.TempDir(), "missing"))
	if ok || n != 0 {
		t.Errorf("activeFrom(missing) = (%d, %v), want (0, false)", n, ok)
	}
}

func TestActiveRejectsNonVTNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "active")
	if err := os.WriteFile(path, []byte("ttyseven\n"), 0o644); err != nil {
		t.Fatalf("writing active file: %v", err)
	}
	if n, ok := activeFrom(path); ok || n != 0 {
		t.Errorf("activeFrom(non-numeric) = (%d, %v), want (0, false)", n, ok)
	}
	if err := os.WriteFile(path, []byte("pts/3\n"), 0o644); err != nil {
		t.Fatalf("writing active file: %v", err)
	}
	if n, ok := activeFrom(path); ok || n != 0 {
		t.Errorf("activeFrom(pts) = (%d, %v), want (0, false)", n, ok)
	}
}

// fakeProc creates a /proc-like tree with one stat line per "process".
func fakeProc(t *testing.T, stats ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i, stat := range stats {
		pdir := filepath.Join(dir, itoa(i+100))
		if err := os.Mkdir(pdir, 0o755); err != nil {
			t.Fatalf("making fake process dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(pdir, "stat"), []byte(stat), 0o644); err != nil {
			t.Fatalf("writing fake stat: %v", err)
		}
	}
	return dir
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for ; n > 0; n /= 10 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
	}
	return string(digits)
}
