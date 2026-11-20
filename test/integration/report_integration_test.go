//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/interpret"
	"github.com/ElenaFri/bootlore/internal/journal"
	"github.com/ElenaFri/bootlore/internal/report"
)

// reportMeta is fixed provenance so the generated report is deterministic.
var reportMeta = report.Meta{
	Tool:      "bootlore",
	Version:   "test",
	Generated: time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC),
}

// interpretedFixture parses the shared boot journal and interprets every event,
// exactly as the report command does before building a report.
func interpretedFixture(t *testing.T) []event.Event {
	t.Helper()
	events, err := journal.ParseWithOrigin(openFixture(t), event.OriginHistorical)
	if err != nil {
		t.Fatalf("ParseWithOrigin: %v", err)
	}
	out := make([]event.Event, len(events))
	for i, e := range events {
		out[i] = interpret.Interpret(e)
	}
	return out
}

// The parse -> interpret -> summary/timing/services -> report pipeline must turn
// the real fixture into a report that carries the same figures the individual
// commands report: five events, one failed bluetooth.service.
func TestReportPipelineFromFixture(t *testing.T) {
	r := report.Build(reportMeta, interpretedFixture(t))

	if r.Summary.Total != 5 {
		t.Errorf("Summary.Total = %d, want 5", r.Summary.Total)
	}
	if r.Summary.Failures != 1 {
		t.Errorf("Summary.Failures = %d, want 1", r.Summary.Failures)
	}
	if len(r.Services.Failed) != 1 || r.Services.Failed[0].Unit != "bluetooth.service" {
		t.Errorf("Services.Failed = %+v, want one bluetooth.service failure", r.Services.Failed)
	}
}

// The JSON rendering of the fixture must be valid and expose the same diagnosis
// so tooling can consume exactly what the console shows.
func TestReportJSONFromFixture(t *testing.T) {
	out, err := report.FormatJSON(report.Build(reportMeta, interpretedFixture(t)))
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, out)
	}

	boot := got["boot"].(map[string]any)
	if boot["id"] != "9f1c2d" {
		t.Errorf("boot.id = %v, want 9f1c2d", boot["id"])
	}
	if boot["events"] != float64(5) || boot["failures"] != float64(1) {
		t.Errorf("boot = %+v, want events 5 / failures 1", boot)
	}
	failed := got["services"].(map[string]any)["failed"].([]any)
	if len(failed) != 1 || failed[0].(map[string]any)["unit"] != "bluetooth.service" {
		t.Errorf("services.failed = %v, want one bluetooth.service failure", failed)
	}
}

// The text and markdown renderings of the fixture must carry every section and
// name the failed unit.
func TestReportTextAndMarkdownFromFixture(t *testing.T) {
	r := report.Build(reportMeta, interpretedFixture(t))

	text := report.FormatText(r)
	for _, want := range []string{"BootLore diagnostic report", "Boot summary", "Boot timing", "bluetooth.service"} {
		if !strings.Contains(text, want) {
			t.Errorf("text report missing %q:\n%s", want, text)
		}
	}

	md := report.FormatMarkdown(r)
	if !strings.HasPrefix(md, "# BootLore diagnostic report") {
		t.Errorf("markdown should start with the top-level heading:\n%s", md)
	}
	for _, want := range []string{"## Summary", "## Services", "bluetooth.service"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown report missing %q:\n%s", want, md)
		}
	}
}
