package tui

import (
	"testing"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/knowledge"
)

// newTestEvent builds an already-interpreted event, as the tui commands receive
// them.
func newTestEvent(message, unit string) event.Event {
	return event.Event{
		Message:            message,
		Unit:               unit,
		Source:             "systemd",
		Priority:           event.PriorityInformational,
		Category:           event.CategorySystemd,
		Significance:       event.SignificanceNormal,
		MonotonicTimestamp: 1200 * 1000 * 1000,
	}
}

func newModel(events ...event.Event) *Model {
	m := New()
	m.Resize(80, 24)
	for _, e := range events {
		m.Append(e)
	}
	return m
}

func TestAppendBuildsVisibleList(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"), newTestEvent("two", "b.service"))
	if m.Count() != 2 {
		t.Fatalf("Count() = %d, want 2", m.Count())
	}
	if m.AllCount() != 2 {
		t.Errorf("AllCount() = %d, want 2", m.AllCount())
	}
}

func TestKeyMovesCursor(t *testing.T) {
	for _, key := range []string{"down", "j"} {
		m := newModel(newTestEvent("one", "a.service"), newTestEvent("two", "b.service"), newTestEvent("three", "c.service"))
		m.Key(key)
		if m.Cursor() != 1 {
			t.Errorf("after %q: cursor = %d, want 1", key, m.Cursor())
		}
		m.Key(key)
		if m.Cursor() != 2 {
			t.Errorf("after second %q: cursor = %d, want 2", key, m.Cursor())
		}
	}
	{
		m := newModel(newTestEvent("one", "a.service"), newTestEvent("two", "b.service"), newTestEvent("three", "c.service"))
		m.Key("end")
		if m.Cursor() != 2 {
			t.Errorf("after end: cursor = %d, want 2", m.Cursor())
		}
		m.Key("down") // clamped at the end
		if m.Cursor() != 2 {
			t.Errorf("cursor past the end: %d", m.Cursor())
		}
		m.Key("home")
		if m.Cursor() != 0 {
			t.Errorf("after home: cursor = %d, want 0", m.Cursor())
		}
		m.Key("up") // clamped at the top
		if m.Cursor() != 0 {
			t.Errorf("cursor before the top: %d", m.Cursor())
		}
	}
}

func TestKeyPgUpDownUsePageSize(t *testing.T) {
	m := newModel()
	for i := 0; i < 30; i++ {
		m.Append(newTestEvent("event", "x.service"))
	}
	m.Key("end")
	if m.Cursor() != 29 {
		t.Fatalf("cursor = %d, want 29", m.Cursor())
	}
	m.Key("pgup")
	if want := 29 - m.PageSize(); m.Cursor() != want {
		t.Errorf("after pgup: cursor = %d, want %d (page = %d)", m.Cursor(), want, m.PageSize())
	}
	m.Key("pgdn")
	if m.Cursor() != 29 {
		t.Errorf("after pgdn: cursor = %d, want 29", m.Cursor())
	}
}

func TestKeysQuitAndViewSwitch(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	if m.Quitting() {
		t.Fatal("fresh model must not be quitting")
	}
	for _, key := range []string{"q", "ctrl-c"} {
		m = newModel(newTestEvent("one", "a.service"))
		m.Key(key)
		if !m.Quitting() {
			t.Errorf("after %q: model should be quitting", key)
		}
	}

	m = newModel(newTestEvent("one", "a.service"))
	if m.View() != ViewLog {
		t.Fatalf("default view = %v, want LOG", m.View())
	}
	m.Key("2")
	if m.View() != ViewHuman {
		t.Errorf("after 2: view = %v, want HUMAN", m.View())
	}
	m.Key("1")
	if m.View() != ViewLog {
		t.Errorf("after 1: view = %v, want LOG", m.View())
	}
}

// The detail pane is the event inspection: opened on the selected event, closed
// with Enter or Esc, and Tab cycles the explanation level.
func TestDetailPane(t *testing.T) {
	m := newModel(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("down") // no-op but must not open the pane
	if m.Detail() {
		t.Error("down must not open the detail pane")
	}
	m.Key("enter")
	if !m.Detail() {
		t.Fatal("enter should open the detail pane")
	}
	if m.Level() != knowledge.LevelTechnical {
		t.Errorf("default level = %v, want technical", m.Level())
	}
	m.Key("tab")
	if m.Level() != knowledge.LevelInDepth {
		t.Errorf("after tab: level = %v, want in-depth", m.Level())
	}
	m.Key("tab")
	if m.Level() != knowledge.LevelOverview {
		t.Errorf("after tab: level = %v, want overview", m.Level())
	}
	m.Key("tab")
	if m.Level() != knowledge.LevelTechnical {
		t.Errorf("tab should cycle: level = %v, want technical", m.Level())
	}
	m.Key("esc")
	if m.Detail() {
		t.Error("esc should close the detail pane")
	}
}

func TestDetailPaneOnEmptyModel(t *testing.T) {
	m := newModel()
	m.Key("enter")
	if m.Detail() {
		t.Error("enter on an empty list must not open the pane")
	}
}

// Filtering: "/" starts an inline edit, Enter applies (recomputing the visible
// list over the whole history), Esc cancels, and an invalid spec keeps the
// previous filter with an error in the status line.
func TestFilterEditing(t *testing.T) {
	network := newTestEvent("Started Network Manager.", "NetworkManager.service")
	network.Category = event.CategoryNetwork
	storage := newTestEvent("Mounted /boot.", "boot.mount")
	storage.Category = event.CategoryStorage
	m := newModel(network, storage)

	m.Key("/")
	if !m.FilterEditing() {
		t.Fatal("'/' should start filter editing")
	}
	for _, r := range "network" {
		m.Key(string(r))
	}
	if spec := m.FilterBuffer(); spec != "network" {
		t.Errorf("typed filter = %q, want %q", spec, "network")
	}
	m.Key("backspace")
	if m.FilterBuffer() != "networ" {
		t.Errorf("after backspace: buffer = %q, want %q", m.FilterBuffer(), "networ")
	}
	m.Key("k")
	m.Key("enter")

	if got := m.Count(); got != 1 {
		t.Errorf("after filtering to network: visible = %d, want 1", got)
	}
	if m.AllCount() != 2 {
		t.Errorf("history must be kept: AllCount = %d, want 2", m.AllCount())
	}
	if m.FilterEditing() {
		t.Error("enter should close filter editing")
	}

	// Esc clears the filter back to everything.
	m.Key("esc")
	if m.Count() != 2 {
		t.Errorf("after clearing the filter: visible = %d, want 2", m.Count())
	}

	// An invalid filter keeps the previous state and reports the error.
	m.Key("/")
	for _, r := range "bogus" {
		m.Key(string(r))
	}
	m.Key("enter")
	if m.Count() != 2 {
		t.Errorf("invalid filter must not drop events: visible = %d, want 2", m.Count())
	}
	if m.Status() == "" {
		t.Error("an invalid filter should leave a message in the status line")
	}
	if !m.FilterEditing() {
		t.Error("an invalid filter should leave editing open for correction")
	}
	m.Key("esc") // cancel editing
	if m.FilterEditing() {
		t.Error("esc should cancel filter editing")
	}
}

// A filter set from the command line applies from the start, and events that
// do not match are still kept in the history for later un-filtering.
func TestSetFilterAndAppend(t *testing.T) {
	network := newTestEvent("Started Network Manager.", "NetworkManager.service")
	network.Category = event.CategoryNetwork
	other := newTestEvent("opaque", "")

	m := New()
	m.Resize(80, 24)
	if err := m.SetFilter("network"); err != nil {
		t.Fatalf("SetFilter(network): %v", err)
	}
	m.Append(network)
	m.Append(other)
	if m.Count() != 1 {
		t.Errorf("visible = %d, want 1 (only network)", m.Count())
	}
	if m.AllCount() != 2 {
		t.Errorf("history = %d, want 2", m.AllCount())
	}
	m.Key("esc") // clears the filter
	if m.Count() != 2 {
		t.Errorf("after clear: visible = %d, want 2", m.Count())
	}
}

func TestSetFilterInvalid(t *testing.T) {
	m := New()
	if err := m.SetFilter("bogus"); err == nil {
		t.Fatal("SetFilter(bogus) should fail")
	}
}

// SetView chooses the starting representation, so the TUI (and the service)
// can open directly in HUMAN narration instead of LOG.
func TestSetView(t *testing.T) {
	m := New()
	if m.View() != ViewLog {
		t.Fatalf("default view = %v, want LOG", m.View())
	}
	m.SetView(ViewHuman)
	if m.View() != ViewHuman {
		t.Errorf("after SetView(HUMAN): view = %v, want HUMAN", m.View())
	}
	// The 1/2 keys still work on top of the chosen starting view.
	m.Key("1")
	if m.View() != ViewLog {
		t.Errorf("after 1: view = %v, want LOG", m.View())
	}
}

// The cursor must stay within the visible window: scrolling down moves the
// window, scrolling up brings it back.
func TestScrollingWindow(t *testing.T) {
	m := newModel()
	for i := 0; i < 40; i++ {
		m.Append(newTestEvent("event", "x.service"))
	}
	m.Key("end")
	if m.Top() != 40-m.PageSize() {
		t.Errorf("Top() = %d, want %d", m.Top(), 40-m.PageSize())
	}
	m.Key("home")
	if m.Top() != 0 {
		t.Errorf("Top() = %d, want 0", m.Top())
	}
	// Shrinking the terminal keeps the cursor visible.
	m.Resize(80, 5)
	if m.Cursor() < m.Top() || m.Cursor() >= m.Top()+m.PageSize() {
		t.Errorf("cursor %d outside window [%d,%d) after resize", m.Cursor(), m.Top(), m.Top()+m.PageSize())
	}
}
