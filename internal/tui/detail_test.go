package tui

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// typeKeys feeds a string to the model one character at a time.
func typeKeys(m *Model, s string) {
	for _, r := range s {
		m.Key(string(r))
	}
}

func frameText(f Frame) string {
	return strings.Join(renderedStrings(f), "\n")
}

// Inside the detail pane the arrow keys scroll the explanation: they must not
// move the selection underneath, and the offset stays within the content.
func TestDetailScrolling(t *testing.T) {
	m := New()
	m.Resize(40, 8) // 6 body rows: the in-depth explanation overflows
	m.Append(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("enter")
	m.Key("tab") // in-depth: the longest explanation

	if m.DetailOffset() != 0 {
		t.Fatalf("a freshly opened pane starts at the top, got %d", m.DetailOffset())
	}

	m.Key("end")
	max := m.DetailOffset()
	if max <= 0 {
		t.Fatalf("the in-depth text should overflow 6 rows (max offset %d)", max)
	}
	m.Key("down")
	m.Key("j")
	if m.DetailOffset() != max {
		t.Errorf("scrolling past the end moved the offset to %d, want it clamped at %d", m.DetailOffset(), max)
	}

	m.Key("home")
	if m.DetailOffset() != 0 {
		t.Errorf("home: offset = %d, want 0", m.DetailOffset())
	}
	m.Key("up")
	m.Key("k")
	if m.DetailOffset() != 0 {
		t.Errorf("scrolling above the top moved the offset to %d", m.DetailOffset())
	}

	m.Key("down")
	m.Key("j")
	if m.DetailOffset() != 2 {
		t.Errorf("two steps down: offset = %d, want 2", m.DetailOffset())
	}
	m.Key("up")
	m.Key("k")
	if m.DetailOffset() != 0 {
		t.Errorf("two steps up: offset = %d, want 0", m.DetailOffset())
	}

	m.Key("pgdn")
	want := 6
	if max < want {
		want = max
	}
	if m.DetailOffset() != want {
		t.Errorf("pgdn: offset = %d, want %d (a page is the 6 body rows)", m.DetailOffset(), want)
	}
	m.Key("pgup")
	if m.DetailOffset() != 0 {
		t.Errorf("pgup: offset = %d, want 0", m.DetailOffset())
	}

	if m.Cursor() != 0 {
		t.Errorf("scrolling the detail must not move the selection, cursor = %d", m.Cursor())
	}
}

// Closing and reopening the pane starts again from the top.
func TestDetailScrollResetsOnReopen(t *testing.T) {
	m := New()
	m.Resize(40, 8)
	m.Append(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("enter")
	m.Key("tab")
	m.Key("end")
	if m.DetailOffset() == 0 {
		t.Fatal("precondition: the pane should have scrolled")
	}
	m.Key("esc")
	m.Key("enter")
	if m.DetailOffset() != 0 {
		t.Errorf("reopened pane offset = %d, want 0", m.DetailOffset())
	}
}

// Changing the explanation level changes the content length; the offset must
// not point past the new end.
func TestDetailOffsetClampedWhenLevelShrinksContent(t *testing.T) {
	m := New()
	m.Resize(40, 8)
	m.Append(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("enter")
	m.Key("tab") // in-depth
	m.Key("end")
	long := m.DetailOffset()
	m.Key("tab") // overview: much shorter
	if m.DetailOffset() >= long {
		t.Errorf("offset %d should shrink with the content (was %d)", m.DetailOffset(), long)
	}
	if got := Render(m, 40, 8); len(got.Lines) != 8 {
		t.Errorf("the pane must still fill the screen, got %d lines", len(got.Lines))
	}
}

// Filtering down to nothing while the pane is open must not leave the pane
// pointing at a vanished event (that used to index an empty list).
func TestDetailClosesWhenFilterEmptiesTheList(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service")) // category SYSTEMD
	m.Key("enter")
	m.Key("/")
	typeKeys(m, "kernel")
	m.Key("enter")

	if m.Count() != 0 {
		t.Fatalf("precondition: the filter should empty the list, got %d", m.Count())
	}
	if m.Detail() {
		t.Error("the detail pane should close when its event is filtered away")
	}
	if got := Render(m, 80, 10); len(got.Lines) != 10 {
		t.Errorf("rendering after emptying must still work, got %d lines", len(got.Lines))
	}
}

func TestMovingOnEmptyListKeepsCursorAtZero(t *testing.T) {
	m := newModel()
	for _, key := range []string{"down", "up", "pgdn", "pgup", "end", "home", "j", "k", "G", "g"} {
		m.Key(key)
		if m.Cursor() != 0 {
			t.Errorf("after %q on an empty list: cursor = %d, want 0", key, m.Cursor())
		}
	}
}

func TestEscapeWithNothingToCloseIsHarmless(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	m.Key("esc")
	if m.Detail() || m.FilterEditing() || m.Quitting() || m.Count() != 1 {
		t.Error("esc with nothing to close must change nothing")
	}
}

func TestTabOutsideDetailKeepsTheLevel(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	before := m.Level()
	m.Key("tab")
	if m.Level() != before {
		t.Errorf("tab outside the detail pane changed the level to %v", m.Level())
	}
}

func TestGAndShiftGJumpToEnds(t *testing.T) {
	m := newModel(newTestEvent("1", "a.service"), newTestEvent("2", "a.service"), newTestEvent("3", "a.service"))
	m.Key("G")
	if m.Cursor() != 2 {
		t.Errorf("G: cursor = %d, want 2", m.Cursor())
	}
	m.Key("g")
	if m.Cursor() != 0 {
		t.Errorf("g: cursor = %d, want 0", m.Cursor())
	}
}

// Leaving the filter editor must not leave a stale error in the status bar.
func TestCancellingTheFilterEditorClearsTheError(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	m.Key("/")
	typeKeys(m, "bogus")
	m.Key("enter")
	if m.Status() == "" {
		t.Fatal("precondition: an invalid filter should set an error")
	}
	m.Key("esc")
	if m.Status() != "" {
		t.Errorf("status = %q after cancelling, want it cleared", m.Status())
	}
}

// --- rendering of the pieces above ---

// With long content the pane shows the top first and the end after scrolling;
// the explanation is word-wrapped, never cut mid-word.
func TestRenderDetailScrolled(t *testing.T) {
	m := New()
	m.Resize(40, 8)
	m.Append(newTestEvent("Started Network Manager.", "NetworkManager.service"))
	m.Key("enter")
	m.Key("tab") // in-depth

	top := frameText(Render(m, 40, 8))
	if !strings.Contains(top, "[info] systemd: Started") {
		t.Errorf("the top of the pane should show the event itself:\n%s", top)
	}
	if strings.Contains(top, "status.") {
		t.Errorf("the end of the explanation should be out of view at first:\n%s", top)
	}

	m.Key("end")
	bottom := frameText(Render(m, 40, 8))
	if strings.Contains(bottom, "[info] systemd: Started") {
		t.Errorf("after scrolling to the end the event itself should be out of view:\n%s", bottom)
	}
	if !strings.Contains(bottom, "status.") {
		t.Errorf("the end of the explanation should be visible after scrolling:\n%s", bottom)
	}
	if got := Render(m, 40, 8); len(got.Lines) != 8 {
		t.Errorf("got %d lines, want exactly the 8 of the terminal", len(got.Lines))
	}
}

// An error from an invalid filter must be visible in the status bar while the
// editor is still open, so the user can correct it.
func TestRenderFilterErrorVisibleWhileEditing(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	m.Key("/")
	typeKeys(m, "bogus")
	m.Key("enter")

	f := Render(m, 140, 10)
	last := f.Lines[len(f.Lines)-1].Text
	for _, want := range []string{"filter: bogus", "unknown category", "Esc:cancel"} {
		if !strings.Contains(last, want) {
			t.Errorf("status bar %q should contain %q", last, want)
		}
	}
}

// Accented text must survive truncation and wrapping intact, whatever the width.
func TestRenderKeepsAccentedTextValid(t *testing.T) {
	msg := "Échec: impossible d'ouvrir le fichier — accès refusé pour l'utilisateur"
	m := New()
	m.Resize(30, 8)
	m.Append(newTestEvent(msg, "a.service"))

	check := func(label string, f Frame) {
		for _, l := range f.Lines {
			if !utf8.ValidString(l.Text) {
				t.Errorf("%s: invalid UTF-8 in %q", label, l.Text)
			}
			if utf8.RuneCountInString(l.Text) > 30 {
				t.Errorf("%s: line wider than the terminal: %q", label, l.Text)
			}
		}
	}
	check("list", Render(m, 30, 8))
	m.Key("enter")
	check("detail", Render(m, 30, 8))
}

// The event the pane shows is the *selected* one.
func TestRenderDetailShowsTheSelectedEvent(t *testing.T) {
	m := newModel(newTestEvent("first event", "a.service"), newTestEvent("second event", "b.service"))
	m.Key("down")
	m.Key("enter")
	text := frameText(Render(m, 80, 24))
	if !strings.Contains(text, "second event") || strings.Contains(text, "first event") {
		t.Errorf("the pane should show the second event only:\n%s", text)
	}
}

// SetStatus shows a message in the status bar until a stray key clears it.
func TestSetStatusShowsInStatusBar(t *testing.T) {
	m := newModel(newTestEvent("one", "a.service"))
	m.SetStatus("journal stream ended")
	if m.Status() != "journal stream ended" {
		t.Fatalf("Status() = %q", m.Status())
	}
	f := Render(m, 100, 10)
	if last := f.Lines[len(f.Lines)-1].Text; !strings.Contains(last, "journal stream ended") {
		t.Errorf("status bar %q should show the message", last)
	}
	m.Key("x")
	if m.Status() != "" {
		t.Errorf("a stray key should clear the message, got %q", m.Status())
	}
}

func TestSizeReportsTheLastResize(t *testing.T) {
	m := New()
	m.Resize(120, 40)
	if w, h := m.Size(); w != 120 || h != 40 {
		t.Errorf("Size() = %dx%d, want 120x40", w, h)
	}
}

// The pane always has at least one body row, even on a terminal too short for
// its header and status bar.
func TestDetailRows(t *testing.T) {
	for h, want := range map[int]int{24: 22, 10: 8, 3: 1, 2: 1, 1: 1, 0: 1, -5: 1} {
		if got := detailRows(h); got != want {
			t.Errorf("detailRows(%d) = %d, want %d", h, got, want)
		}
	}
}
