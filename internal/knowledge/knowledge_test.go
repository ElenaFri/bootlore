package knowledge

import (
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
)

func TestLevelString(t *testing.T) {
	cases := map[Level]string{
		LevelOverview:  "overview",
		LevelTechnical: "technical",
		LevelInDepth:   "in-depth",
	}
	for l, want := range cases {
		if got := l.String(); got != want {
			t.Errorf("Level(%d).String() = %q, want %q", int8(l), got, want)
		}
	}
	if got := Level(42).String(); got != "unknown" {
		t.Errorf("Level(42).String() = %q, want unknown", got)
	}
}

func TestEntryTextPerLevel(t *testing.T) {
	e := Entry{Overview: "overview text", Technical: "technical text", InDepth: "in-depth text"}
	cases := map[Level]string{
		LevelOverview:  "overview text",
		LevelTechnical: "technical text",
		LevelInDepth:   "in-depth text",
	}
	for l, want := range cases {
		if got := e.Text(l); got != want {
			t.Errorf("Text(%s) = %q, want %q", l, got, want)
		}
	}
	if got := e.Text(Level(42)); got != "" {
		t.Errorf("Text(invalid level) = %q, want empty", got)
	}
}

// The knowledge base is maintained entry by entry: every entry must be complete
// (all three explanation levels, a summary) and carry a stable ID.
func TestEntriesAreComplete(t *testing.T) {
	all := All()
	if len(all) == 0 {
		t.Fatal("knowledge base is empty")
	}
	for _, e := range all {
		if e.ID == "" {
			t.Error("entry with empty ID")
		}
		for name, field := range map[string]string{
			"Summary":   e.Summary,
			"Overview":  e.Overview,
			"Technical": e.Technical,
			"InDepth":   e.InDepth,
		} {
			if field == "" {
				t.Errorf("entry %q: %s is empty", e.ID, name)
			}
		}
	}
}

// Every matching rule must point at an existing entry.
func TestRulesReferenceExistingEntries(t *testing.T) {
	for _, r := range rules {
		if _, ok := entries[r.entryID]; !ok {
			t.Errorf("rule points at unknown entry %q", r.entryID)
		}
	}
}

func TestForMatchesKnownBootMessages(t *testing.T) {
	cases := []struct {
		name string
		e    event.Event
		want string // expected entry ID, "" for no match
	}{
		{
			"kernel version line",
			event.Event{Source: "kernel", Message: "Linux version 6.6.8-arch1-1 (linux@archlinux) #1 SMP PREEMPT_DYNAMIC"},
			"kernel-version",
		},
		{
			"systemd starting",
			event.Event{Source: "systemd", Message: "systemd 254.5-1-arch running in system mode."},
			"systemd-start",
		},
		{
			"unit started",
			event.Event{Source: "systemd", Unit: "NetworkManager.service", Message: "Started Network Manager."},
			"unit-lifecycle",
		},
		{
			"unit starting",
			event.Event{Source: "systemd", Message: "Starting Network Manager..."},
			"unit-lifecycle",
		},
		{
			"unit finished",
			event.Event{Source: "systemd", Message: "Finished Rebuild Dynamic Linker Cache."},
			"unit-lifecycle",
		},
		{
			"target reached",
			event.Event{Source: "systemd", Message: "Reached target Multi-User System."},
			"target-reached",
		},
		{
			"mount unit",
			event.Event{Source: "systemd", Unit: "boot.mount", Message: "Mounted /boot."},
			"filesystem-mounted",
		},
		{
			"kernel ext4 mount",
			event.Event{Source: "kernel", Message: "EXT4-fs (sda2): mounted filesystem with ordered data mode."},
			"filesystem-mounted",
		},
		{
			"service failed with result",
			event.Event{Source: "systemd", Unit: "bluetooth.service", Message: "bluetooth.service: Failed with result 'exit-code'."},
			"service-failed",
		},
		{
			"failed to start",
			event.Event{Source: "systemd", Unit: "foo.service", Message: "Failed to start Foo."},
			"service-failed",
		},
		{
			"opaque daemon message",
			event.Event{Source: "some-daemon", Message: "doing something opaque"},
			"",
		},
		{
			"empty event",
			event.Event{},
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			entry, ok := For(tc.e)
			if tc.want == "" {
				if ok {
					t.Fatalf("For() matched %q, want no match", entry.ID)
				}
				return
			}
			if !ok {
				t.Fatalf("For() did not match, want entry %q", tc.want)
			}
			if entry.ID != tc.want {
				t.Errorf("For() matched %q, want %q", entry.ID, tc.want)
			}
		})
	}
}

// Transparency principle (architecture §2.3): the failure entry must clearly
// separate the established fact from the cause, which is not stated by the
// message and requires investigation.
func TestServiceFailedEntrySeparatesFactFromCause(t *testing.T) {
	entry, ok := For(event.Event{Source: "systemd", Unit: "x.service", Message: "Failed with result 'exit-code'."})
	if !ok {
		t.Fatal("expected a match")
	}
	if !strings.Contains(entry.Technical, "not an explanation") {
		t.Errorf("service-failed technical text should state that the failure result is not an explanation of the cause:\n%s", entry.Technical)
	}
	if !strings.Contains(entry.InDepth, "hypothes") {
		t.Errorf("service-failed in-depth text should label possible causes as hypotheses:\n%s", entry.InDepth)
	}
}
