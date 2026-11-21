package doctor

import (
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/timing"
)

func TestAllPass(t *testing.T) {
	evts := systemBootEvents()
	checks := Analyze(Events(evts, 3))

	if len(checks) != len(CheckOrder) {
		t.Fatalf("got %d checks, want %d", len(checks), len(CheckOrder))
	}
	for _, c := range checks {
		if c.Status != Pass {
			t.Errorf("%s: status = %v, want Pass", c.Name, c.Status)
		}
		if c.Hint != "" {
			t.Errorf("%s: hint = %q, want empty", c.Name, c.Hint)
		}
	}
}

func TestBackendUnavailableFailsEverything(t *testing.T) {
	checks := Analyze(BackendErr(ErrBackend))

	if len(checks) == 0 {
		t.Fatal("got no checks")
	}
	for _, c := range checks {
		if c.Status != Fail {
			t.Errorf("%s: status = %v, want Fail", c.Name, c.Status)
		}
	}
}

func TestNoEventsMeansUnreadableJournal(t *testing.T) {
	checks := Analyze(Events(nil, 0))

	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	// The backend ran fine; everything that reads the journal did not.
	if c := byName["journalctl"]; c.Status != Pass {
		t.Errorf("journalctl: status = %v, want Pass", c.Status)
	}
	for _, name := range []string{"journal", "boot-events", "boots"} {
		c := byName[name]
		if c.Status == Pass {
			t.Errorf("%s: status = Pass, want not Pass", name)
		}
	}
}

func TestUserOnlyJournalFailsSystemJournalCheck(t *testing.T) {
	evts := userSessionEvents()
	checks := Analyze(Events(evts, 1))

	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	if c := byName["journalctl"]; c.Status != Pass {
		t.Errorf("journalctl: status = %v, want Pass", c.Status)
	}
	if c := byName["journal"]; c.Status != Fail {
		t.Errorf("journal: status = %v, want Fail", c.Status)
	}
	if !strings.Contains(byName["journal"].Hint, "systemd-journal") {
		t.Errorf("journal: hint = %q, want mention of systemd-journal group", byName["journal"].Hint)
	}

	// Preconditions: the events must be detected as user-session-only by
	// BootLore's own heuristics, mirroring what timing reports.
	if timing.OnlyUserSession(evts) != true {
		t.Error("precondition: events must be detected as user-only")
	}
}

func TestUserOnlyJournalStillCountsEvents(t *testing.T) {
	evts := userSessionEvents()
	checks := Analyze(Events(evts, 1))

	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	// journalctl runs and events come back; the boot check has data even
	// though the *system* journal check fails.
	if c := byName["boot-events"]; c.Status != Pass {
		t.Errorf("boot-events: status = %v, want Pass", c.Status)
	}
}

func TestSingleBootWarnsCompare(t *testing.T) {
	evts := systemBootEvents()
	checks := Analyze(Events(evts, 1))

	byName := map[string]Check{}
	for _, c := range checks {
		byName[c.Name] = c
	}

	if c := byName["boots"]; c.Status != Warn {
		t.Errorf("boots: status = %v, want Warn", c.Status)
	}
	if !strings.Contains(byName["boots"].Hint, "compare") {
		t.Errorf("boots: hint = %q, want mention of compare", byName["boots"].Hint)
	}
}

func TestRenderedOutput(t *testing.T) {
	evts := systemBootEvents()
	checks := Analyze(Events(evts, 1))
	out := Render(checks)

	for _, c := range checks {
		if !strings.Contains(out, c.Name) {
			t.Errorf("render missing check %q", c.Name)
		}
		if !strings.Contains(out, c.Status.String()) {
			t.Errorf("render missing status %q of %s", c.Status, c.Name)
		}
	}
}

func TestWorstPrefersFailureOverWarning(t *testing.T) {
	mixed := []Check{
		{Name: "a", Status: Pass},
		{Name: "b", Status: Warn},
		{Name: "c", Status: Fail},
		{Name: "d", Status: Warn},
	}
	if got := Worst(mixed); got != Fail {
		t.Errorf("Worst(mixed) = %v, want Fail", got)
	}
	if got := Worst([]Check{{Status: Pass}, {Status: Warn}}); got != Warn {
		t.Errorf("Worst(pass,warn) = %v, want Warn", got)
	}
	if got := Worst(nil); got != Pass {
		t.Errorf("Worst(nil) = %v, want Pass", got)
	}
}

func TestStatusStringUnknownIsFail(t *testing.T) {
	// An out-of-range status must not render as an empty word: the CLI
	// relies on the status word to communicate problems.
	if got := Status(42).String(); got != "fail" {
		t.Errorf("Status(42).String() = %q, want fail", got)
	}
}

func systemBootEvents() []event.Event {
	return []event.Event{
		{Message: "Startup finished in 1.2s.", Fields: map[string]string{
			"_PID":       "1",
			"_UID":       "0",
			"_COMM":      "systemd",
			"SYSLOG_PID": "1",
		}},
	}
}

func userSessionEvents() []event.Event {
	return []event.Event{
		{Message: "Reached target Main User Target.", Fields: map[string]string{
			"_SYSTEMD_USER_UNIT": "main.target",
			"_PID":               "1340",
			"_UID":               "1000",
			"_COMM":              "systemd",
		}},
	}
}
