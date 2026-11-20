package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ElenaFri/bootlore/internal/render"
	"github.com/ElenaFri/bootlore/internal/summary"
)

// FormatMarkdown renders the report as shareable Markdown: a top-level heading,
// a provenance line, and a section per analysis using headings, bullet points
// and tables. It is meant to be pasted into a bug report or issue. The output
// has no trailing newline.
func FormatMarkdown(r Report) string {
	var b strings.Builder
	b.WriteString("# BootLore diagnostic report\n\n")
	b.WriteString(provenanceMarkdown(r.Meta))

	markdownSummary(&b, r)
	markdownTiming(&b, r)
	markdownServices(&b, r)

	return strings.TrimRight(b.String(), "\n")
}

// provenanceMarkdown renders the one-line provenance paragraph.
func provenanceMarkdown(m Meta) string {
	tool := m.Tool
	if tool == "" {
		tool = "bootlore"
	}
	switch {
	case !m.Generated.IsZero() && m.Version != "":
		return fmt.Sprintf("Generated `%s` by %s %s.\n", utc(m.Generated), tool, m.Version)
	case !m.Generated.IsZero():
		return fmt.Sprintf("Generated `%s` by %s.\n", utc(m.Generated), tool)
	case m.Version != "":
		return fmt.Sprintf("Produced by %s %s.\n", tool, m.Version)
	default:
		return fmt.Sprintf("Produced by %s.\n", tool)
	}
}

// markdownSummary renders the summary section: the headline metrics as a table,
// and the per-subsystem breakdown as a second table.
func markdownSummary(b *strings.Builder, r Report) {
	s := r.Summary
	b.WriteString("\n## Summary\n\n")

	b.WriteString("| Metric | Value |\n| --- | --- |\n")
	mdRow(b, "Boot ID", summary.BootLabel(s))
	mdRow(b, "Events", strconv.Itoa(s.Total))
	if s.Total == 0 {
		b.WriteString("\n_No events to report._\n")
		return
	}
	mdRow(b, "Normal", strconv.Itoa(s.Normal))
	mdRow(b, "Warnings", strconv.Itoa(s.Warnings))
	mdRow(b, "Failures", strconv.Itoa(s.Failures))
	if s.Unassessed > 0 {
		mdRow(b, "Unassessed", strconv.Itoa(s.Unassessed))
	}
	if !s.First.IsZero() {
		mdRow(b, "Observed", fmt.Sprintf("%s to %s (span %s)", utc(s.First), utc(s.Last), render.Duration(s.Span)))
	}
	if s.Elapsed > 0 {
		mdRow(b, "Into boot", render.FormatOffset(s.Elapsed))
	}

	if cats := summary.OrderedCategories(s); len(cats) > 0 {
		b.WriteString("\n### By subsystem\n\n")
		b.WriteString("| Subsystem | Events |\n| --- | --- |\n")
		for _, c := range cats {
			mdRow(b, summary.CategoryLabel(c), strconv.Itoa(s.Categories[c]))
		}
	}
}

// markdownTiming renders the timing section: the total with its source, then the
// per-phase breakdown and reached milestones as tables when present.
func markdownTiming(b *strings.Builder, r Report) {
	t := r.Timing
	b.WriteString("\n## Timing\n\n")
	if t.Total == 0 && len(t.Milestones) == 0 {
		b.WriteString("_No boot timing information available._\n")
		return
	}

	fmt.Fprintf(b, "Total **%s** (%s).\n", render.Duration(t.Total), timingSourceText(t))

	if len(t.Phases) > 0 {
		b.WriteString("\n### Phases\n\n")
		b.WriteString("| Phase | Duration |\n| --- | --- |\n")
		for _, p := range t.Phases {
			mdRow(b, p.Name, render.Duration(p.Duration))
		}
	}

	b.WriteString("\n### Milestones\n\n")
	if len(t.Milestones) == 0 {
		b.WriteString("_None observed._\n")
		return
	}
	b.WriteString("| Offset | Target |\n| --- | --- |\n")
	for _, m := range t.Milestones {
		mdRow(b, render.FormatOffset(m.At), m.Name)
	}
}

// markdownServices renders the services section: failed units and reconstructed
// slow startups, each as a table or a short note when empty.
func markdownServices(b *strings.Builder, r Report) {
	rep := r.Services
	b.WriteString("\n## Services\n\n")

	b.WriteString("### Failed services\n\n")
	if len(rep.Failed) == 0 {
		b.WriteString("_No failed services._\n")
	} else {
		b.WriteString("| Unit | At | Count | Message |\n| --- | --- | --- | --- |\n")
		for _, f := range rep.Failed {
			fmt.Fprintf(b, "| %s | %s | %d | %s |\n",
				mdCell(f.Unit), mdCell(render.FormatOffset(f.At)), f.Count, mdCell(f.Message))
		}
	}

	b.WriteString("\n### Slow services (slowest first)\n\n")
	if len(rep.Slow) == 0 {
		b.WriteString("_No service startup times could be reconstructed._\n")
		return
	}
	b.WriteString("| Duration | Started | Service |\n| --- | --- | --- |\n")
	for _, s := range rep.Slow {
		fmt.Fprintf(b, "| %s | %s | %s |\n",
			mdCell(render.Duration(s.Duration)), mdCell(render.FormatOffset(s.Started)), mdCell(s.Name))
	}
}

// mdRow writes a two-column "| key | value |" markdown table row, escaping both
// cells.
func mdRow(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "| %s | %s |\n", mdCell(key), mdCell(value))
}

// mdCell makes a string safe to place inside a markdown table cell: pipes would
// end the cell and newlines would end the row, so both are neutralised.
func mdCell(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
