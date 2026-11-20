// Package report assembles BootLore's per-boot diagnostics into a single,
// exportable report and renders it in several formats.
//
// A report bundles the three single-boot analyses the individual commands
// produce -- the summary (counts and significance), the timing (duration,
// phases and milestones) and the services diagnosis (failed units and
// reconstructed startup times) -- together with provenance metadata (which tool
// produced it, which version and when). Because it reuses those analyses
// verbatim, the figures a report shows are exactly those `bootlore summary`,
// `bootlore timing` and `bootlore services` report.
//
// Three renderings are offered:
//
//   - text: the console report, the three sub-reports under a provenance header;
//   - markdown: a shareable report with headings and tables, for pasting into a
//     bug report or issue;
//   - json: a stable, documented schema for tooling and archival.
//
// Build is pure (it never mutates the events); the Format* functions are pure
// too, so the whole package is trivially testable from recorded fixtures.
package report

import (
	"fmt"
	"strings"
	"time"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/services"
	"github.com/ElenaFri/bootlore/internal/summary"
	"github.com/ElenaFri/bootlore/internal/timing"
)

// Meta is a report's provenance: which tool produced it, which version and when.
// Any field may be left zero; the renderers degrade gracefully.
type Meta struct {
	Tool      string
	Version   string
	Generated time.Time
}

// Report is a complete single-boot diagnostic: the boot's summary, its timing
// and its systemd-service diagnosis, with provenance metadata.
type Report struct {
	Meta     Meta
	Summary  summary.Summary
	Timing   timing.Timing
	Services services.Report
}

// Build assembles a diagnostic report from interpreted events (their Category
// and Significance filled, exactly as summary and services expect). It never
// modifies the events.
func Build(meta Meta, events []event.Event) Report {
	return Report{
		Meta:     meta,
		Summary:  summary.Summarize(events),
		Timing:   timing.Analyze(events),
		Services: services.Analyze(events),
	}
}

// Format renders a report in the named format: "text" (the default, also used
// for the empty string), "markdown" or "json". An unknown format is an error.
func Format(r Report, format string) (string, error) {
	switch format {
	case "", "text":
		return FormatText(r), nil
	case "markdown", "md":
		return FormatMarkdown(r), nil
	case "json":
		return FormatJSON(r)
	default:
		return "", fmt.Errorf("unknown report format %q (want text, markdown or json)", format)
	}
}

// FormatText renders the report as the console report: the three sub-reports,
// exactly as their own commands print them, under a provenance header. It has no
// trailing newline.
func FormatText(r Report) string {
	var b strings.Builder
	b.WriteString("BootLore diagnostic report\n")
	b.WriteString(provenanceLine(r.Meta))
	b.WriteString("\n\n")
	b.WriteString(summary.Format(r.Summary))
	b.WriteString("\n\n")
	b.WriteString(timing.Format(r.Timing))
	b.WriteString("\n\n")
	b.WriteString(services.Format(r.Services))
	return b.String()
}

// provenanceLine renders the indented provenance sub-line of the text header,
// showing whichever of the when/tool/version pieces are known.
func provenanceLine(m Meta) string {
	tool := m.Tool
	if tool == "" {
		tool = "bootlore"
	}
	switch {
	case !m.Generated.IsZero() && m.Version != "":
		return fmt.Sprintf("  Generated %s by %s %s", utc(m.Generated), tool, m.Version)
	case !m.Generated.IsZero():
		return fmt.Sprintf("  Generated %s by %s", utc(m.Generated), tool)
	case m.Version != "":
		return fmt.Sprintf("  %s %s", tool, m.Version)
	default:
		return "  " + tool
	}
}

// utc renders an instant as a UTC RFC3339 timestamp.
func utc(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// timingSourceText describes where a timing total came from, matching the phrase
// the timing command prints.
func timingSourceText(t timing.Timing) string {
	if t.FromSummary {
		return "reported by systemd"
	}
	return "observed; systemd reported no startup summary"
}

// timingSourceJSON is the machine-readable source tag: "reported" when systemd's
// authoritative summary supplied the total, "observed" when it was inferred.
func timingSourceJSON(t timing.Timing) string {
	if t.FromSummary {
		return "reported"
	}
	return "observed"
}
