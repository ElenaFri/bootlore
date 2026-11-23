// Guarding the optional systemd unit file: it encodes the safety rules of the
// automatic startup mode (architecture.md §6), and these tests keep it honest.
// They are file reads, like the man/README drift checks: fast, deterministic.
package docs

import (
	"regexp"
	"strings"
	"testing"
)

const unitFile = "../../systemd/bootlore.service"

func TestUnitFileExists(t *testing.T) {
	page := readFile(t, unitFile)
	if !strings.Contains(page, "[Unit]") {
		t.Fatalf("%s does not look like a systemd unit:\n%s", unitFile, page)
	}
}

// The unit must run under a dedicated unprivileged account — never root.
func TestUnitRunsUnderDedicatedAccount(t *testing.T) {
	unit := readFile(t, unitFile)
	user := regexp.MustCompile(`(?m)^User=(\S+)$`).FindStringSubmatch(unit)
	if user == nil {
		t.Fatal("unit must set User= (a dedicated unprivileged account)")
	}
	if user[1] == "root" {
		t.Error("unit must not run as root")
	}
	if user[1] != "bootlore" {
		t.Errorf("User = %q, want the documented bootlore account", user[1])
	}
}

// BootLore never blocks or delays system startup: the journal dependency is a
// mere ordering (After=), never a requirement (Requires=), and the unit never
// orders itself before another unit.
func TestUnitNeverBlocksBoot(t *testing.T) {
	unit := readFile(t, unitFile)
	if !regexp.MustCompile(`(?m)^After=.*systemd-journald\.service`).MatchString(unit) {
		t.Error("unit must order itself after systemd-journald.service")
	}
	if regexp.MustCompile(`(?m)^Requires=`).MatchString(unit) {
		t.Error("unit must not Requires= anything: a late journal may not delay or block the monitor")
	}
	if regexp.MustCompile(`(?m)^Before=`).MatchString(unit) {
		t.Error("unit must never order itself before another unit (never delay boot)")
	}
}

// The TUI runs on a terminal systemd hands it: TTYPath and --vt must exist and
// name the same tty, otherwise the service starts a TUI that refuses its own
// assignment.
func TestUnitAgreesOnTheTerminal(t *testing.T) {
	unit := readFile(t, unitFile)
	tty := regexp.MustCompile(`(?m)^TTYPath=/dev/tty(\d+)$`).FindStringSubmatch(unit)
	vt := regexp.MustCompile(`(?m)^ExecStart=.*--vt (\d+)`).FindStringSubmatch(unit)
	if tty == nil {
		t.Fatal("unit must set TTYPath=/dev/ttyN for the TUI")
	}
	if vt == nil {
		t.Fatal("unit must start the TUI with --vt N (verify the assigned terminal)")
	}
	if tty[1] != vt[1] {
		t.Errorf("TTYPath names tty%s but ExecStart verifies --vt %s: they must agree", tty[1], vt[1])
	}
	if !regexp.MustCompile(`(?m)^StandardInput=tty$`).MatchString(unit) {
		t.Error("unit must hand the terminal to the process on standard input")
	}
}

// An occupied or unavailable terminal is a normal, graceful exit; a restart
// loop would fight for a terminal that will not free itself, hog the journal
// and spam the log on every boot.
func TestUnitDoesNotRestart(t *testing.T) {
	unit := readFile(t, unitFile)
	restart := regexp.MustCompile(`(?m)^Restart=(\S+)$`).FindStringSubmatch(unit)
	if restart == nil {
		t.Fatal("unit must set Restart= explicitly")
	}
	if restart[1] != "no" {
		t.Errorf("Restart = %q, want no (unavailable terminals are a graceful exit)", restart[1])
	}
}

// Journal access is optional and granted through the standard group, not
// through privilege: the unit asks for the group, nothing more.
func TestUnitJournalAccessIsGroupBased(t *testing.T) {
	unit := readFile(t, unitFile)
	if !regexp.MustCompile(`(?m)^SupplementaryGroups=systemd-journal$`).MatchString(unit) {
		t.Error("unit must grant journal access via SupplementaryGroups=systemd-journal")
	}
}
