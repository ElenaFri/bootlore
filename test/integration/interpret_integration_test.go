//go:build integration

package integration

import (
	"os"
	"strings"
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
	"github.com/ElenaFri/bootlore/internal/journal"
)

// A user-session journal is shaped differently from a system boot journal:
// every entry carries the user manager as its hosting unit and most systemd
// messages are about *another* unit (USER_UNIT). This fixture was distilled
// from a real journal, where classifying by the hosting unit put every event
// under SYSTEMD. The parse -> classify -> assess -> explain pipeline must tell
// the subsystems apart.
func TestInterpretUserSessionJournal(t *testing.T) {
	f, err := os.Open("../testdata/user-session-journal.json")
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	events, err := journal.ParseWithOrigin(f, event.OriginHistorical)
	if err != nil {
		t.Fatalf("ParseWithOrigin: %v", err)
	}

	want := []struct {
		message  string
		category event.Category
		signif   event.Significance
	}{
		{"Started Multimedia Service Session Manager.", event.CategoryDesktop, event.SignificanceNormal},
		{"Reached target Paths.", event.CategorySystemd, event.SignificanceNormal},
		{"Dependency failed for Portal service.", event.CategoryDesktop, event.SignificanceFailure},
		{"Service file", event.CategoryOther, event.SignificanceWarning},
		{"couldn't access control socket", event.CategoryDesktop, event.SignificanceWarning},
		{"Process 155721", event.CategorySystemd, event.SignificanceFailure},
		{"Created TensorFlow Lite", event.CategoryUnclassified, event.SignificanceNormal},
		{"pam_unix(login:session)", event.CategorySystemd, event.SignificanceNormal},
	}
	if len(events) != len(want) {
		t.Fatalf("got %d events, want %d", len(events), len(want))
	}

	seen := map[event.Category]int{}
	for i, w := range want {
		e := interpret.Interpret(events[i])
		seen[e.Category]++
		if !strings.HasPrefix(e.Message, w.message) {
			t.Fatalf("events[%d].Message = %q, fixture order changed (want prefix %q)", i, e.Message, w.message)
		}
		if e.Category != w.category {
			t.Errorf("%q: Category = %q, want %q", w.message, e.Category, w.category)
		}
		if e.Significance != w.signif {
			t.Errorf("%q: Significance = %v, want %v", w.message, e.Significance, w.signif)
		}
	}

	// The point of the fixture: the corpus must spread across subsystems rather
	// than collapse into one bucket.
	if len(seen) < 4 {
		t.Errorf("events fell into only %d categories (%v); classification collapsed", len(seen), seen)
	}
	if seen[event.CategoryUnclassified] == 0 {
		t.Error("an application BootLore knows nothing about must stay unclassified")
	}
}
