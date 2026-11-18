package tui

import (
	"fmt"
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/render"
)

// Line is one rendered screen row: plain text (never escape sequences or
// colours), plus a Reverse marker the terminal layer turns into reverse video —
// which stays usable on monochrome terminals.
type Line struct {
	Text    string
	Reverse bool
}

// Frame is a full screen.
type Frame struct {
	Width  int
	Height int
	Lines  []Line
}

// minWidth/minHeight are the smallest terminal the TUI can do anything useful
// with; below that it only shows a message.
const (
	minWidth  = 20
	minHeight = 3
)

// Render draws the model into a full frame of plain-text lines. Lines are
// truncated to the width; content is never invented, only cut.
func Render(m *Model, w, h int) Frame {
	f := Frame{Width: w, Height: h}
	if w < minWidth || h < minHeight {
		f.Lines = []Line{{Text: fmt.Sprintf("Terminal too small (need %dx%d, have %dx%d)", minWidth, minHeight, w, h)}}
		return f
	}

	if m.Detail() {
		f.Lines = renderDetail(m, w, h)
	} else {
		f.Lines = renderList(m, w, h)
	}
	// All lines must fit the terminal exactly.
	for i := range f.Lines {
		f.Lines[i].Text = truncate(f.Lines[i].Text, w)
	}
	return f
}

func renderList(m *Model, w, h int) []Line {
	rows := h - 1 // status bar takes the last line
	lines := make([]Line, 0, h)

	if m.Count() == 0 {
		lines = append(lines, Line{Text: "No events yet — waiting for journal input…"})
		for len(lines) < rows-1 {
			lines = append(lines, Line{Text: ""})
		}
	} else {
		for i := 0; i < rows; i++ {
			idx := m.Top() + i
			if idx >= m.Count() {
				lines = append(lines, Line{Text: ""})
				continue
			}
			e := m.Visible(idx)
			var body func(event.Event) string
			if m.View() == ViewHuman {
				body = render.HumanBody
			} else {
				body = render.LogBody
			}
			lines = append(lines, Line{
				Text:    render.TimelineLine(e, body),
				Reverse: idx == m.Cursor(),
			})
		}
	}

	lines = append(lines, Line{Text: statusLine(m, m.Count(), m.AllCount()), Reverse: true})
	return lines
}

func renderDetail(m *Model, w, h int) []Line {
	e := m.Visible(m.Cursor())
	lines := make([]Line, 0, h)

	header := fmt.Sprintf("Event %d/%d", m.Cursor()+1, m.Count())
	if meta := detailMeta(e); meta != "" {
		header += " · " + meta
	}
	lines = append(lines, Line{Text: header + " · Tab:level Esc:back q:quit", Reverse: true})

	body := []string{
		"LOG     " + render.LogLine(e),
		"HUMAN   " + render.HumanLine(e),
	}
	entry, ok := knowledge.For(e)
	if !ok {
		body = append(body, "EXPLAIN (no explanation available for this event; the original message is above)")
	} else {
		level := m.Level()
		body = append(body, fmt.Sprintf("EXPLAIN [%s] %s", level, entry.Text(level)))
	}

	shown := 0
	for _, para := range body {
		for _, ln := range wrap(para, w) {
			if shown >= m.detailLine {
				lines = append(lines, Line{Text: ln})
			}
			shown++
			if len(lines) >= h-1 {
				goto done
			}
		}
	}
done:
	for len(lines) < h-1 {
		lines = append(lines, Line{Text: ""})
	}
	lines = append(lines, Line{Text: statusLine(m, m.Count(), m.AllCount()), Reverse: true})
	return lines
}

func detailMeta(e event.Event) string {
	parts := []string{}
	if e.Origin != "" {
		parts = append(parts, string(e.Origin))
	}
	if e.IsClassified() {
		parts = append(parts, string(e.Category))
	}
	return strings.Join(parts, " · ")
}

func statusLine(m *Model, visible, total int) string {
	view := "LOG"
	if m.View() == ViewHuman {
		view = "HUMAN"
	}
	if m.FilterEditing() {
		return fmt.Sprintf("filter: %s · Enter:apply Esc:cancel", m.FilterBuffer())
	}
	filter := "no filter"
	if spec := m.FilterSpec(); spec != "" {
		filter = "filter:" + spec
	}
	status := m.Status()
	if status != "" {
		filter = status
	}
	return fmt.Sprintf("%s · %s · %d/%d events · 1:LOG 2:HUMAN Enter:detail /:filter q:quit",
		view, filter, visible, total)
}

func truncate(s string, w int) string {
	if len(s) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	if w == 2 {
		return s[:1] + "…"
	}
	return s[:w-3] + "…"
}

func wrap(s string, w int) []string {
	var out []string
	for _, para := range strings.Split(s, "\n") {
		for {
			if len(para) <= w {
				out = append(out, para)
				break
			}
			out = append(out, para[:w])
			para = para[w:]
		}
	}
	return out
}
