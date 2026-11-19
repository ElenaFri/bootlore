package tui

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/knowledge"
)

func renderedStrings(f Frame) []string {
	out := make([]string, len(f.Lines))
	for i, l := range f.Lines {
		out[i] = l.Text
	}
	return out
}

// Rendered output must be pure text: no escape sequences (monochrome output is
// achieved by never emitting colours; reverse video is applied by the terminal
// layer, not by the renderer).
func TestRenderNeverEmitsEscapeSequences(t *testing.T) {
	m := newModel(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("enter")
	for _, w := range []int{10, 80} {
		for _, h := range []int{3, 24} {
			for _, l := range Render(m, w, h).Lines {
				if strings.Contains(l.Text, "\x1b") {
					t.Errorf("render contains an escape sequence: %q", l.Text)
				}
			}
		}
	}
}

// Every line must fit the terminal width, whatever the event content.
func TestRenderTruncatesToWidth(t *testing.T) {
	m := newModel(newTestEvent(strings.Repeat("long ", 100)+"end", "a.service"))
	f := Render(m, 40, 10)
	for _, l := range f.Lines {
		if utf8.RuneCountInString(l.Text) > 40 {
			t.Errorf("line exceeds the width: %q", l.Text)
		}
	}
	// Nothing is silently invented: the truncation must have dropped content,
	// not added it.
	joined := strings.Join(renderedStrings(f), "\n")
	if !strings.Contains(joined, "long") {
		t.Error("truncated output should still show the beginning of the message")
	}
}

// Terminals smaller than the minimum get an explicit, single-line message.
func TestRenderTooSmall(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	f := Render(m, 10, 2)
	if len(f.Lines) != 1 || !strings.Contains(f.Lines[0].Text, "Terminal too small") {
		t.Fatalf("got %+v, want a single too-small message", f.Lines)
	}
}

func TestRenderListShowsViewAndSelection(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"), newTestEvent("two", "b.service"))
	m.Key("2") // HUMAN view
	f := Render(m, 80, 10)
	if len(f.Lines) != 10 {
		t.Fatalf("got %d lines, want 10 (list rows + status bar)", len(f.Lines))
	}
	if !f.Lines[0].Reverse {
		t.Error("the first row (cursor) should be marked as selected")
	}
	if !strings.Contains(f.Lines[0].Text, "one") {
		t.Errorf("first row = %q", f.Lines[0].Text)
	}
	if strings.Contains(f.Lines[1].Text, "two") == false {
		t.Errorf("second row = %q", f.Lines[1].Text)
	}
	status := f.Lines[len(f.Lines)-1]
	if !status.Reverse {
		t.Error("the status bar should be marked as reversed")
	}
	for _, want := range []string{"HUMAN", "2/2", "Enter", "q:quit"} {
		if !strings.Contains(status.Text, want) {
			t.Errorf("status bar %q should mention %q", status.Text, want)
		}
	}
}

func TestRenderListLogView(t *testing.T) {
	m := newModel(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	f := Render(m, 80, 10)
	if !strings.Contains(f.Lines[0].Text, "[info]") {
		t.Errorf("LOG view should show the priority: %q", f.Lines[0].Text)
	}
}

// The empty state must say that we are waiting, not draw a blank screen.
func TestRenderEmptyList(t *testing.T) {
	m := newModel()
	f := Render(m, 80, 10)
	joined := strings.Join(renderedStrings(f), "\n")
	if !strings.Contains(joined, "No events") {
		t.Errorf("empty list should show a waiting message, got:\n%s", joined)
	}
}

// The detail pane is the event inspection: LOG, HUMAN and EXPLAIN at the
// selected level, plus metadata.
func TestRenderDetailPane(t *testing.T) {
	e := newTestEvent("Started Network Manager.", "NetworkManager.service")
	e.Human = "A systemd unit changed state as part of the boot sequence."
	e.Category = event.CategoryNetwork
	e.Origin = event.OriginLive
	m := newModel(e)
	m.Key("enter")

	f := Render(m, 80, 24)
	joined := strings.Join(renderedStrings(f), "\n")
	for _, want := range []string{"LOG", "HUMAN", "EXPLAIN", "technical", "live", "NETWORK"} {
		if !strings.Contains(joined, want) {
			t.Errorf("detail pane should show %q:\n%s", want, joined)
		}
	}
	if !strings.Contains(joined, "A systemd unit changed state") {
		t.Errorf("detail pane should show the human description:\n%s", joined)
	}
}

// Tab changes the depth of the EXPLAIN section.
func TestRenderDetailPaneLevels(t *testing.T) {
	e := newTestEvent("Started Network Manager.", "NetworkManager.service")
	m := newModel(e)
	m.Key("enter")

	withLevel := func() string {
		return strings.Join(renderedStrings(Render(m, 100, 30)), "\n")
	}
	if !strings.Contains(withLevel(), "dependency order") { // technical
		t.Errorf("detail pane should start at the technical level:\n%s", withLevel())
	}
	m.Key("tab") // in-depth
	if !strings.Contains(withLevel(), "Type=") {
		t.Errorf("detail pane should show the in-depth level after Tab:\n%s", withLevel())
	}
	m.Key("tab") // overview
	if !strings.Contains(withLevel(), "piece by piece") {
		t.Errorf("detail pane should show the overview level after two Tabs:\n%s", withLevel())
	}
}

// Events without a knowledge entry must say so honestly instead of inventing an
// explanation.
func TestRenderDetailPaneWithoutKnowledge(t *testing.T) {
	m := newModel(newTestEvent("doing something opaque", ""))
	m.Key("enter")
	joined := strings.Join(renderedStrings(Render(m, 80, 24)), "\n")
	if !strings.Contains(joined, "no explanation") {
		t.Errorf("unknown event should state that no explanation is available:\n%s", joined)
	}
	if knowledge.HasEntryFor(event.Event{Message: "doing something opaque"}) {
		t.Error("the test event should not match a knowledge entry")
	}
}

// The filter editor draws what the user typed and is reflected in the status
// bar; once applied, the status bar shows the active filter.
func TestRenderFilterEditing(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	m.Key("/")
	m.Key("n")
	m.Key("e")
	f := Render(m, 80, 10)
	joined := strings.Join(renderedStrings(f), "\n")
	if !strings.Contains(joined, "filter: ne") || !strings.Contains(joined, "Enter:apply") {
		t.Errorf("status bar should show the filter being typed and its keys:\n%s", joined)
	}
}

func TestRenderStatusShowsFilter(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	if err := m.SetFilter("systemd"); err != nil {
		t.Fatal(err)
	}
	f := Render(m, 80, 10)
	if !strings.Contains(f.Lines[len(f.Lines)-1].Text, "systemd") {
		t.Errorf("status bar should show the active filter: %q", f.Lines[len(f.Lines)-1].Text)
	}
}

// A small terminal still gets a usable screen: rows shrink, nothing panics.
func TestRenderSmallTerminal(t *testing.T) {
	m := newModel()
	for i := 0; i < 30; i++ {
		m.Append(newTestEvent(strings.Repeat("x", 30), "u.service"))
	}
	m.Key("end")
	f := Render(m, 20, 4)
	if len(f.Lines) != 4 {
		t.Fatalf("got %d lines, want 4", len(f.Lines))
	}
	for _, l := range f.Lines {
		if utf8.RuneCountInString(l.Text) > 20 {
			t.Errorf("line exceeds width: %q", l.Text)
		}
	}
}
