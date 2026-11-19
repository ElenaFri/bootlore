package tui

import (
	"fmt"
	"strings"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/knowledge"
	"github.com/ElenaFri/bootlore/internal/render"
)

// Line is one rendered screen row: plain text (never escape sequences or
// colours), plus a Reverse marker the terminal layer turns into reverse video,
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

	if m.Detail() && m.Count() > 0 {
		f.Lines = renderDetail(m, w, h)
	} else {
		f.Lines = renderList(m, h)
	}
	// Every line must fit the terminal.
	for i := range f.Lines {
		f.Lines[i].Text = truncate(f.Lines[i].Text, w)
	}
	return f
}

func renderList(m *Model, h int) []Line {
	rows := h - 1 // the status bar takes the last line
	lines := make([]Line, 0, h)

	if m.Count() == 0 {
		lines = append(lines, Line{Text: "No events yet — waiting for journal input…"})
		for len(lines) < rows {
			lines = append(lines, Line{})
		}
	} else {
		body := render.LogBody
		if m.View() == ViewHuman {
			body = render.HumanBody
		}
		for i := 0; i < rows; i++ {
			idx := m.Top() + i
			if idx >= m.Count() {
				lines = append(lines, Line{})
				continue
			}
			lines = append(lines, Line{
				Text:    render.TimelineLine(m.Visible(idx), body),
				Reverse: idx == m.Cursor(),
			})
		}
	}

	return append(lines, Line{Text: statusLine(m), Reverse: true})
}

// detailRows is the number of body rows of the detail pane on a terminal h
// lines high: the header and the status bar take two.
func detailRows(h int) int {
	if h-2 < 1 {
		return 1
	}
	return h - 2
}

// detailBody is the content of the detail pane, wrapped to w: the LOG and HUMAN
// representations of the event and its EXPLAIN section at the given level. The
// explanation is the knowledge base's own text; when there is none, the pane
// says so rather than inventing one.
func detailBody(e event.Event, level knowledge.Level, w int) []string {
	sections := []string{
		"LOG     " + render.LogLine(e),
		"HUMAN   " + render.HumanLine(e),
	}
	if entry, ok := knowledge.For(e); ok {
		sections = append(sections, fmt.Sprintf("EXPLAIN [%s] %s", level, entry.Text(level)))
	} else {
		sections = append(sections, "EXPLAIN (no explanation available for this event; the original message is above)")
	}

	var lines []string
	for _, s := range sections {
		lines = append(lines, wrap(s, w)...)
	}
	return lines
}

func renderDetail(m *Model, w, h int) []Line {
	e := m.Visible(m.Cursor())

	header := fmt.Sprintf("Event %d/%d", m.Cursor()+1, m.Count())
	if meta := detailMeta(e); meta != "" {
		header += " · " + meta
	}
	header += " · Tab:level j/k:scroll Esc:back q:quit"

	body := detailBody(e, m.Level(), w)
	rows := detailRows(h)

	// Clamp here too: the terminal may have shrunk since the offset was set.
	offset := m.DetailOffset()
	if max := len(body) - rows; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}

	lines := make([]Line, 0, h)
	lines = append(lines, Line{Text: header, Reverse: true})
	for i := 0; i < rows; i++ {
		text := ""
		if idx := offset + i; idx < len(body) {
			text = body[idx]
		}
		lines = append(lines, Line{Text: text})
	}
	return append(lines, Line{Text: statusLine(m), Reverse: true})
}

func detailMeta(e event.Event) string {
	var parts []string
	if e.Origin != "" {
		parts = append(parts, string(e.Origin))
	}
	if e.IsClassified() {
		parts = append(parts, string(e.Category))
	}
	return strings.Join(parts, " · ")
}

// statusLine is the bottom bar. While the filter editor is open it shows the
// text being typed first and the keys, then any error, so that the keys stay
// visible on a narrow terminal and the error is the part that gets cut.
func statusLine(m *Model) string {
	if m.FilterEditing() {
		line := fmt.Sprintf("filter: %s · Enter:apply Esc:cancel", m.FilterBuffer())
		if s := m.Status(); s != "" {
			line += " · " + s
		}
		return line
	}

	view := "LOG"
	if m.View() == ViewHuman {
		view = "HUMAN"
	}
	note := "no filter"
	if spec := m.FilterSpec(); spec != "" {
		note = "filter:" + spec
	}
	if s := m.Status(); s != "" {
		note = s
	}
	return fmt.Sprintf("%s · %s · %d/%d events · 1:LOG 2:HUMAN Enter:detail /:filter q:quit",
		view, note, m.Count(), m.AllCount())
}
