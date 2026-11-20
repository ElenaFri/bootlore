package report

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
)

// base is a fixed wall-clock instant used to build deterministic fixtures.
var base = time.Date(2024, 1, 2, 10, 0, 0, 0, time.UTC)

// sampleMeta is fixed provenance so the generated timestamp is deterministic.
func sampleMeta() Meta {
	return Meta{Tool: "bootlore", Version: "1.2.3", Generated: base}
}

// sampleBoot is a small interpreted boot exercising all three analyses the
// report bundles: summary counts, systemd timing (a startup summary plus a
// milestone) and services (a failed unit and a measurable startup).
func sampleBoot() []event.Event {
	return []event.Event{
		{BootID: "boot-a", RealtimeTimestamp: base, MonotonicTimestamp: 1 * time.Second,
			Message: "Starting Network Manager...", Category: event.CategoryNetwork, Significance: event.SignificanceNormal},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(2 * time.Second), MonotonicTimestamp: 3 * time.Second,
			Message: "Started Network Manager.", Category: event.CategoryNetwork, Significance: event.SignificanceNormal},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(4 * time.Second), MonotonicTimestamp: 4 * time.Second,
			Message: "Reached target Basic System.", Category: event.CategorySystemd, Significance: event.SignificanceNormal},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(5 * time.Second), MonotonicTimestamp: 6 * time.Second,
			Message: "bluetooth.service: Failed with result 'exit-code'.", Category: event.CategoryHardware, Significance: event.SignificanceFailure},
		{BootID: "boot-a", RealtimeTimestamp: base.Add(6 * time.Second), MonotonicTimestamp: 7 * time.Second,
			Message: "Startup finished in 1.500s (kernel) + 3.500s (userspace) = 5.000s.", Category: event.CategorySystemd, Significance: event.SignificanceNormal},
	}
}

// Build assembles the three per-boot analyses and carries the provenance,
// without modifying the events.
func TestBuildAssemblesAnalyses(t *testing.T) {
	r := Build(sampleMeta(), sampleBoot())

	if r.Meta.Version != "1.2.3" || r.Meta.Tool != "bootlore" {
		t.Errorf("Meta = %+v, want tool bootlore / version 1.2.3", r.Meta)
	}
	if r.Summary.Total != 5 {
		t.Errorf("Summary.Total = %d, want 5", r.Summary.Total)
	}
	if r.Summary.Failures != 1 {
		t.Errorf("Summary.Failures = %d, want 1", r.Summary.Failures)
	}
	if !r.Timing.FromSummary || r.Timing.Total != 5*time.Second {
		t.Errorf("Timing = {FromSummary:%v Total:%v}, want {true 5s}", r.Timing.FromSummary, r.Timing.Total)
	}
	if len(r.Services.Failed) != 1 || r.Services.Failed[0].Unit != "bluetooth.service" {
		t.Errorf("Services.Failed = %+v, want one bluetooth.service failure", r.Services.Failed)
	}
	if len(r.Services.Slow) != 1 || r.Services.Slow[0].Name != "Network Manager" {
		t.Errorf("Services.Slow = %+v, want one Network Manager timing", r.Services.Slow)
	}
}

// The text report gathers all three sub-reports under a provenance header.
func TestFormatTextContainsEverySection(t *testing.T) {
	out := FormatText(Build(sampleMeta(), sampleBoot()))

	for _, want := range []string{
		"BootLore diagnostic report",
		"bootlore 1.2.3",
		"2024-01-02T10:00:00Z",
		"Boot summary",
		"Boot timing",
		"Failed services",
		"bluetooth.service",
		"Network Manager",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text report missing %q\n--- report ---\n%s", want, out)
		}
	}
}

// The markdown report is shareable: real headings and the same facts.
func TestFormatMarkdownIsStructured(t *testing.T) {
	out := FormatMarkdown(Build(sampleMeta(), sampleBoot()))

	if !strings.HasPrefix(out, "# BootLore diagnostic report") {
		t.Errorf("markdown should start with the top-level heading:\n%s", out)
	}
	for _, want := range []string{
		"## Summary",
		"## Timing",
		"## Services",
		"| Metric | Value |",
		"bluetooth.service",
		"Network Manager",
		"bootlore 1.2.3",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown report missing %q\n--- report ---\n%s", want, out)
		}
	}
}

// A message carrying a pipe must not break a markdown table cell.
func TestFormatMarkdownEscapesPipes(t *testing.T) {
	events := []event.Event{
		{BootID: "boot-a", RealtimeTimestamp: base, MonotonicTimestamp: time.Second,
			Message: "cups.service: Failed a | b.", Significance: event.SignificanceFailure},
	}
	out := FormatMarkdown(Build(sampleMeta(), events))
	if !strings.Contains(out, `a \| b`) {
		t.Errorf("markdown should escape the pipe in a table cell:\n%s", out)
	}
}

// The JSON report is valid, documented and carries every figure the text report
// shows, so tooling can consume exactly the same diagnosis.
func TestFormatJSONSchema(t *testing.T) {
	out, err := FormatJSON(Build(sampleMeta(), sampleBoot()))
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("report is not valid JSON: %v\n%s", err, out)
	}

	if got["tool"] != "bootlore" || got["version"] != "1.2.3" {
		t.Errorf("tool/version = %v/%v, want bootlore/1.2.3", got["tool"], got["version"])
	}
	if got["generated"] != "2024-01-02T10:00:00Z" {
		t.Errorf("generated = %v, want 2024-01-02T10:00:00Z", got["generated"])
	}

	boot := got["boot"].(map[string]any)
	if boot["id"] != "boot-a" {
		t.Errorf("boot.id = %v, want boot-a", boot["id"])
	}
	if boot["events"] != float64(5) || boot["failures"] != float64(1) || boot["normal"] != float64(4) {
		t.Errorf("boot counts = %+v, want events 5 / normal 4 / failures 1", boot)
	}
	subsystems := boot["subsystems"].(map[string]any)
	if subsystems["NETWORK"] != float64(2) || subsystems["SYSTEMD"] != float64(2) {
		t.Errorf("subsystems = %+v, want NETWORK 2 / SYSTEMD 2", subsystems)
	}

	timing := got["timing"].(map[string]any)
	if timing["total"] != "5.000s" || timing["source"] != "reported" {
		t.Errorf("timing = %+v, want total 5.000s / source reported", timing)
	}
	phases := timing["phases"].([]any)
	if len(phases) != 2 {
		t.Fatalf("timing.phases = %v, want 2 phases", phases)
	}

	services := got["services"].(map[string]any)
	failed := services["failed"].([]any)
	if len(failed) != 1 {
		t.Fatalf("services.failed = %v, want one failure", failed)
	}
	if failed[0].(map[string]any)["unit"] != "bluetooth.service" {
		t.Errorf("services.failed[0].unit = %v, want bluetooth.service", failed[0])
	}
}

// Format dispatches by name, defaults to text and rejects unknown formats.
func TestFormatDispatch(t *testing.T) {
	r := Build(sampleMeta(), sampleBoot())

	text, err := Format(r, "")
	if err != nil || !strings.Contains(text, "BootLore diagnostic report") {
		t.Errorf("empty format should default to text: err=%v\n%s", err, text)
	}
	if got, _ := Format(r, "text"); got != FormatText(r) {
		t.Error(`Format(r, "text") should equal FormatText(r)`)
	}
	md, _ := Format(r, "markdown")
	if !strings.HasPrefix(md, "# BootLore diagnostic report") {
		t.Errorf(`Format(r, "markdown") should produce markdown:\n%s`, md)
	}
	js, _ := Format(r, "json")
	if !json.Valid([]byte(js)) {
		t.Errorf(`Format(r, "json") should produce valid JSON:\n%s`, js)
	}
	if _, err := Format(r, "yaml"); err == nil {
		t.Error("unknown format should be an error")
	}
}

// Provenance degrades gracefully: any of the tool/version/when pieces may be
// missing, the tool name defaults to "bootlore", and JSON omits what is absent.
func TestProvenanceDegradesGracefully(t *testing.T) {
	text := FormatText(Build(Meta{}, sampleBoot()))
	if !strings.Contains(text, "  bootlore") || strings.Contains(text, "Generated") {
		t.Errorf("empty meta should name the default tool with no timestamp:\n%s", text)
	}

	md := FormatMarkdown(Build(Meta{}, sampleBoot()))
	if !strings.Contains(md, "Produced by bootlore.") {
		t.Errorf("empty meta markdown should fall back to the default tool:\n%s", md)
	}

	versionOnly := FormatText(Build(Meta{Version: "9.9"}, sampleBoot()))
	if !strings.Contains(versionOnly, "bootlore 9.9") || strings.Contains(versionOnly, "Generated") {
		t.Errorf("version-only meta should show the version without a timestamp:\n%s", versionOnly)
	}
	versionOnlyMD := FormatMarkdown(Build(Meta{Version: "9.9"}, sampleBoot()))
	if !strings.Contains(versionOnlyMD, "Produced by bootlore 9.9.") {
		t.Errorf("version-only markdown should show the version without a timestamp:\n%s", versionOnlyMD)
	}

	whenOnly := FormatText(Build(Meta{Generated: base}, sampleBoot()))
	if !strings.Contains(whenOnly, "Generated 2024-01-02T10:00:00Z by bootlore") {
		t.Errorf("when-only meta should show the timestamp and default tool:\n%s", whenOnly)
	}
	whenOnlyMD := FormatMarkdown(Build(Meta{Generated: base}, sampleBoot()))
	if !strings.Contains(whenOnlyMD, "Generated `2024-01-02T10:00:00Z` by bootlore.") {
		t.Errorf("when-only markdown should show the timestamp and default tool:\n%s", whenOnlyMD)
	}

	js, err := FormatJSON(Build(Meta{}, sampleBoot()))
	if err != nil {
		t.Fatalf("FormatJSON: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, js)
	}
	if got["tool"] != "bootlore" {
		t.Errorf("tool = %v, want the default bootlore", got["tool"])
	}
	if _, ok := got["version"]; ok {
		t.Errorf("version should be omitted when empty:\n%s", js)
	}
	if _, ok := got["generated"]; ok {
		t.Errorf("generated should be omitted when zero:\n%s", js)
	}
}

// An empty boot must still produce a valid report in every format.
func TestFormatEmptyBoot(t *testing.T) {
	r := Build(sampleMeta(), nil)

	if out := FormatText(r); !strings.Contains(out, "BootLore diagnostic report") {
		t.Errorf("empty text report should still have a header:\n%s", out)
	}
	if out := FormatMarkdown(r); !strings.HasPrefix(out, "# BootLore diagnostic report") {
		t.Errorf("empty markdown report should still have a heading:\n%s", out)
	}
	out, err := FormatJSON(r)
	if err != nil || !json.Valid([]byte(out)) {
		t.Errorf("empty JSON report should be valid: err=%v\n%s", err, out)
	}
}
