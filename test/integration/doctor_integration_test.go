//go:build integration

package integration

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/collector"
	"github.com/ElenaFri/bootlore/internal/collectortest"
	"github.com/ElenaFri/bootlore/internal/doctor"
)

// stubDoctorJournalctl is a journalctl stub that answers the doctor's two
// probes: a boot listing and the boot's event stream.
func stubDoctorJournalctl(t *testing.T, events, boots string) string {
	t.Helper()
	body := `case "$*" in
  *--list-boots*)
    printf '%s\n' '` + boots + `'
    ;;
  *)
    printf '%s\n' '` + events + `'
    ;;
esac`
	return collectortest.WriteStubJournalctl(t, body)
}

const doctorStubSystemEvent = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1","_UID":"0","MESSAGE":"Startup finished in 1.2s."}`

const doctorStubUserEvent = `{"__REALTIME_TIMESTAMP":"1700000000000000","__MONOTONIC_TIMESTAMP":"1200000","PRIORITY":"6","SYSLOG_IDENTIFIER":"systemd","_PID":"1340","_UID":"1000","_SYSTEMD_USER_UNIT":"main.target","MESSAGE":"Reached target Main User Target."}`

const doctorStubBoots = ` -1 1608c9e8 Sun 2026-11-15 22:44:26 CET Mon 2026-11-16 15:15:43 CET
  0 48be9b9b Fri 2026-11-20 11:01:03 CET Fri 2026-11-20 11:23:01 CET`

// The full doctor pipeline — real exec runner, boot event probe, boot list
// probe, analysis — against a healthy stub system must pass every check.
func TestDoctorPipelineHealthySystem(t *testing.T) {
	dir := stubDoctorJournalctl(t, doctorStubSystemEvent, doctorStubBoots)
	t.Setenv("PATH", dir)

	ctx := context.Background()
	c := collector.New(collector.NewExecRunner())
	events, err := c.Collect(ctx, collector.Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	boots, err := c.BootCount(ctx)
	if err != nil {
		t.Fatalf("BootCount returned error: %v", err)
	}

	checks := doctor.Analyze(doctor.Events(events, boots))
	if got := doctor.Worst(checks); got != doctor.Pass {
		t.Fatalf("healthy stub system should pass, got %v:\n%s", got, doctor.Render(checks))
	}
	out := doctor.Render(checks)
	if !strings.Contains(out, "2 boots recorded") {
		t.Errorf("render should report 2 boots recorded:\n%s", out)
	}
}

// A user-session-only journal (reader not in the systemd-journal group) must
// fail exactly the system-journal check, with the group hint.
func TestDoctorPipelineUserOnlyJournal(t *testing.T) {
	dir := stubDoctorJournalctl(t, doctorStubUserEvent, doctorStubBoots)
	t.Setenv("PATH", dir)

	ctx := context.Background()
	c := collector.New(collector.NewExecRunner())
	events, err := c.Collect(ctx, collector.Options{})
	if err != nil {
		t.Fatalf("Collect returned error: %v", err)
	}
	boots, err := c.BootCount(ctx)
	if err != nil {
		t.Fatalf("BootCount returned error: %v", err)
	}

	checks := doctor.Analyze(doctor.Events(events, boots))
	byName := map[string]doctor.Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}
	if c := byName["journal"]; c.Status != doctor.Fail {
		t.Errorf("journal check = %v, want Fail", c.Status)
	}
	if !strings.Contains(byName["journal"].Hint, "systemd-journal") {
		t.Errorf("journal hint = %q, want the group hint", byName["journal"].Hint)
	}
	if c := byName["boots"]; c.Status != doctor.Pass {
		t.Errorf("boots check = %v, want Pass (the stub lists two boots)", c.Status)
	}
}

// No journalctl at all: the probes surface ErrBackendUnavailable and the
// analysis fails every check without crashing.
func TestDoctorPipelineBackendMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // empty dir: no journalctl anywhere

	c := collector.New(collector.NewExecRunner())
	_, err := c.Collect(context.Background(), collector.Options{})
	if err == nil {
		t.Fatal("Collect should fail without journalctl")
	}
	if !errors.Is(err, collector.ErrBackendUnavailable) {
		t.Fatalf("error = %v, want ErrBackendUnavailable", err)
	}

	checks := doctor.Analyze(doctor.BackendErr(err))
	if got := doctor.Worst(checks); got != doctor.Fail {
		t.Fatalf("missing backend should fail every check, got %v", got)
	}
	if len(checks) != len(doctor.CheckOrder) {
		t.Errorf("got %d checks, want %+v", len(checks), doctor.CheckOrder)
	}
}
