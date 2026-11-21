package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
)

// --- doctor command ---

const doctorBoots = ` -1 1608c9e8 Sun 2026-11-15 22:44:26 CET Mon 2026-11-16 15:15:43 CET
  0 48be9b9b Fri 2026-11-20 11:01:03 CET Fri 2026-11-20 11:23:01 CET
`

const doctorSystemBoot = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","_UID":"0","MESSAGE":"Startup finished in 1.2s."}
`

const doctorUserOnlyBoot = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1340","_UID":"1000","_SYSTEMD_USER_UNIT":"main.target","MESSAGE":"Reached target Main User Target."}
`

// A healthy system: journalctl answers, the system journal is readable, the
// run recorded enough boots to compare.
func TestDoctorHealthySystem(t *testing.T) {
	r := &doctorRunner{Boots: doctorBoots, Events: doctorSystemBoot}
	stdout, stderr, code := runCLI([]string{"doctor"}, strings.NewReader(""), r)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if !strings.Contains(stdout, "journalctl") || !strings.Contains(stdout, "journal") {
		t.Errorf("stdout should list the checks:\n%s", stdout)
	}
	if strings.Contains(stdout, "fail") || strings.Contains(stdout, "warn") {
		t.Errorf("healthy system should report no failure:\n%s", stdout)
	}
	if !strings.Contains(stdout, "2 boots recorded") {
		t.Errorf("stdout should report the boot count:\n%s", stdout)
	}
	if strings.Contains(stdout, "hint") {
		t.Errorf("healthy system needs no hints:\n%s", stdout)
	}
}

// The user sees only their own session journal: journalctl and boot-events
// pass, but the system-journal check fails with the group hint, and doctor
// exits non-zero.
func TestDoctorUserOnlyJournalFailsWithHint(t *testing.T) {
	r := &doctorRunner{Boots: doctorBoots, Events: doctorUserOnlyBoot}
	stdout, _, code := runCLI([]string{"doctor"}, strings.NewReader(""), r)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout, "systemd-journal") {
		t.Errorf("stdout should hint at the systemd-journal group:\n%s", stdout)
	}
}

// No journalctl at all: every check fails and the output still lists them all.
func TestDoctorBackendUnavailableFailsAllChecks(t *testing.T) {
	r := &doctorRunner{Err: fmt.Errorf("journal backend unavailable: journalctl failed: exit status 127")}
	stdout, _, code := runCLI([]string{"doctor"}, strings.NewReader(""), r)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if got := strings.Count(stdout, "  fail  "); got != 4 {
		t.Errorf("all four checks should fail (got %d fail lines):\n%s", got, stdout)
	}
}

// A single recorded boot is a warning (compare needs two), not a failure: the
// exit code stays zero so scripts only fail on real problems.
func TestDoctorSingleBootIsWarningNotFailure(t *testing.T) {
	r := &doctorRunner{Boots: "  0 48be9b9b Fri 2026-11-20 11:01:03 CET Fri 2026-11-20 11:23:01 CET\n", Events: doctorSystemBoot}
	stdout, _, code := runCLI([]string{"doctor"}, strings.NewReader(""), r)
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (warn, not fail)", code)
	}
	if !strings.Contains(stdout, "warn") || !strings.Contains(stdout, "compare") {
		t.Errorf("single boot should warn about compare:\n%s", stdout)
	}
}

func TestDoctorBadFlagFails(t *testing.T) {
	r := &doctorRunner{Boots: doctorBoots, Events: doctorSystemBoot}
	_, _, code := runCLI([]string{"doctor", "--bogus"}, strings.NewReader(""), r)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

// doctor always probes the live system; piping journal JSON at it must not be
// silently accepted as a replacement for the real probes.
func TestDoctorIgnoresPipedJournal(t *testing.T) {
	r := &doctorRunner{Boots: doctorBoots, Events: doctorSystemBoot}
	_, stderr, code := runCLI([]string{"doctor"}, strings.NewReader(twoEntries), r)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if len(r.GotRuns()) != 2 {
		t.Errorf("doctor should probe the backend (boot events + boot list), got %d runs", len(r.GotRuns()))
	}
}

var _ collector.Runner = (*doctorRunner)(nil)
