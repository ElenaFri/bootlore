//go:build integration

// Package integration contains cross-component tests that exercise several
// BootLore packages together against realistic journal fixtures read from disk.
//
// Run with: go test -tags=integration ./test/integration/...
package integration

import (
	"os"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/render"
)

const fixture = "../testdata/boot-journal.json"

// openFixture opens the shared boot journal fixture.
func openFixture(t *testing.T) *os.File {
	t.Helper()
	f, err := os.Open(fixture)
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}

// The collector→parser→model path must turn a real journal export into
// normalized events, preserving order and mapping the well-known fields.
func TestParseBootFixture(t *testing.T) {
	events, err := journal.ParseWithOrigin(openFixture(t), event.OriginHistorical)
	if err != nil {
		t.Fatalf("ParseWithOrigin: %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("got %d events, want 5", len(events))
	}

	// Order of appearance must be preserved.
	wantMessages := []string{
		"Linux version 6.6.8-arch1-1 (linux@archlinux) #1 SMP PREEMPT_DYNAMIC",
		"systemd 254.5-1-arch running in system mode.",
		"EXT4-fs (sda2): mounted filesystem with ordered data mode.",
		"Started Network Manager.",
		"bluetooth.service: Failed with result 'exit-code'.",
	}
	for i, want := range wantMessages {
		if events[i].Message != want {
			t.Errorf("events[%d].Message = %q, want %q", i, events[i].Message, want)
		}
		if events[i].Origin != event.OriginHistorical {
			t.Errorf("events[%d].Origin = %q, want historical", i, events[i].Origin)
		}
		if events[i].BootID != "9f1c2d" {
			t.Errorf("events[%d].BootID = %q, want 9f1c2d", i, events[i].BootID)
		}
	}

	// First entry is a kernel message with no PID.
	first := events[0]
	if first.Source != "kernel" {
		t.Errorf("first.Source = %q, want kernel", first.Source)
	}
	if first.Priority != event.PriorityNotice {
		t.Errorf("first.Priority = %v, want notice", first.Priority)
	}
	if first.PID != 0 {
		t.Errorf("first.PID = %d, want 0", first.PID)
	}
	wantTime := time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)
	if !first.RealtimeTimestamp.Equal(wantTime) {
		t.Errorf("first.RealtimeTimestamp = %v, want %v", first.RealtimeTimestamp, wantTime)
	}
	if first.MonotonicTimestamp != 1200*time.Millisecond {
		t.Errorf("first.MonotonicTimestamp = %v, want 1.2s", first.MonotonicTimestamp)
	}
	if first.Fields["_TRANSPORT"] != "kernel" {
		t.Errorf("first.Fields[_TRANSPORT] = %q, want kernel", first.Fields["_TRANSPORT"])
	}

	// NetworkManager entry carries unit and PID.
	nm := events[3]
	if nm.Source != "NetworkManager" || nm.Unit != "NetworkManager.service" || nm.PID != 412 {
		t.Errorf("NetworkManager entry = %+v", nm)
	}

	// The last entry is an operational warning (priority 4).
	last := events[4]
	if last.Priority != event.PriorityWarning {
		t.Errorf("last.Priority = %v, want warning", last.Priority)
	}
}

// The parsed events must render to faithful LOG lines end-to-end.
func TestRenderBootFixtureLogView(t *testing.T) {
	events, err := journal.Parse(openFixture(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	wantFirst := "2023-11-14T22:13:20Z [notice] kernel: Linux version 6.6.8-arch1-1 (linux@archlinux) #1 SMP PREEMPT_DYNAMIC"
	if got := render.LogLine(events[0]); got != wantFirst {
		t.Errorf("LogLine(first)\n got = %q\nwant = %q", got, wantFirst)
	}

	wantNM := "2023-11-14T22:13:23Z [info] NetworkManager[412]: Started Network Manager."
	if got := render.LogLine(events[3]); got != wantNM {
		t.Errorf("LogLine(nm)\n got = %q\nwant = %q", got, wantNM)
	}
}
