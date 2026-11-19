// Package tui implements BootLore's interactive terminal boot monitor
// (architecture §4.8): it replays the current boot, follows it live, and lets
// the user switch views, navigate, filter and inspect events.
//
// The package is split so that nearly everything is testable:
//
//   - Model holds the state (events, cursor, window, view, filter, detail
//     pane) and applies key presses: pure logic.
//   - Render turns a Model plus terminal dimensions into plain text lines: no
//     colours, truncated to the width, so monochrome and small terminals work
//     by construction.
//   - keys.go and screen.go convert terminal input into key names and frames
//     into bytes: pure functions.
//   - term_linux.go and run_linux.go own the terminal itself (termios via
//     syscall, alternate screen, resize signals); they are exercised in tests
//     through a real pseudo-terminal (see internal/ptytest).
package tui

import (
	"strings"
	"sync"

	"github.com/ElenaFri/bootlore/internal/event"
	"github.com/ElenaFri/bootlore/internal/filter"
	"github.com/ElenaFri/bootlore/internal/knowledge"
)

// View selects which line representation the list shows.
type View int

const (
	ViewLog   View = iota // original messages
	ViewHuman             // BootLore's interpretation
)

// Model is the state of the TUI. Its methods are pure: they mutate the model
// but never touch a terminal. The run loop serializes access with mu.
type Model struct {
	all     []event.Event // full history, never filtered away
	visible []int         // indices into all, matching the filter
	top     int           // first visible row (window into visible)
	cursor  int           // selected row, index into visible
	view    View
	page    int // rows usable for the list (height minus the status bar)

	detail     bool
	detailLine int // first line of the detail pane that is shown
	level      knowledge.Level

	filter     filter.Filter
	filterSpec string
	filing     bool   // filter input active
	editBuf    string // text typed in the filter editor
	status     string // transient message shown in the status bar

	w, h     int
	quitting bool

	// Run-loop glue, unused by the pure tests: mu serializes access to the
	// model, waker asks the loop for a redraw. waker must be set before any
	// goroutine that may call requestRedraw is started.
	mu    sync.Mutex
	waker func()
}

// New returns an empty model; its size is set by Resize.
func New() *Model {
	return &Model{page: 1, level: knowledge.LevelTechnical}
}

// --- state accessors ---

func (m *Model) Count() int             { return len(m.visible) }
func (m *Model) AllCount() int          { return len(m.all) }
func (m *Model) Cursor() int            { return m.cursor }
func (m *Model) Top() int               { return m.top }
func (m *Model) PageSize() int          { return m.page }
func (m *Model) View() View             { return m.view }
func (m *Model) Detail() bool           { return m.detail }
func (m *Model) DetailOffset() int      { return m.detailLine }
func (m *Model) Level() knowledge.Level { return m.level }
func (m *Model) Status() string         { return m.status }
func (m *Model) Quitting() bool         { return m.quitting }

// Size returns the terminal dimensions recorded by Resize.
func (m *Model) Size() (w, h int) { return m.w, m.h }

// FilterEditing reports whether the inline filter editor is open.
func (m *Model) FilterEditing() bool { return m.filing }

// FilterBuffer returns the text currently typed into the filter editor.
func (m *Model) FilterBuffer() string { return m.editBuf }

// FilterSpec returns the active filter specification ("" for none).
func (m *Model) FilterSpec() string { return m.filterSpec }

// Visible returns the i-th visible event.
func (m *Model) Visible(i int) event.Event {
	return m.all[m.visible[i]]
}

// SetStatus sets the transient message shown in the status bar.
func (m *Model) SetStatus(msg string) { m.status = msg }

// --- state transitions ---

// Resize records the terminal dimensions and keeps the cursor and the detail
// pane inside their windows.
func (m *Model) Resize(w, h int) {
	m.w, m.h = w, h
	if page := h - 1; page > 0 {
		m.page = page
	}
	m.clampWindow()
	m.clampDetail()
}

// Append adds an event to the history and, if it passes the filter, to the
// visible list.
func (m *Model) Append(e event.Event) {
	m.all = append(m.all, e)
	if m.filter.Match(e) {
		m.visible = append(m.visible, len(m.all)-1)
	}
}

// SetFilter applies a category filter over the whole history.
func (m *Model) SetFilter(spec string) error {
	f, err := filter.ParseCategories(spec)
	if err != nil {
		return err
	}
	m.filter, m.filterSpec = f, strings.TrimSpace(spec)
	m.recompute()
	return nil
}

func (m *Model) recompute() {
	m.visible = m.visible[:0]
	for i := range m.all {
		if m.filter.Match(m.all[i]) {
			m.visible = append(m.visible, i)
		}
	}
	if len(m.visible) == 0 {
		m.detail = false // the event the pane showed is gone
	}
	m.clampCursor()
	m.clampWindow()
	m.clampDetail()
}

func (m *Model) clampCursor() {
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

func (m *Model) clampWindow() {
	max := len(m.visible) - m.page
	if m.top > max {
		m.top = max
	}
	if m.top < 0 || len(m.visible) == 0 {
		m.top = 0
	}
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.page > 0 && m.cursor >= m.top+m.page {
		m.top = m.cursor - m.page + 1
	}
}

// Key applies a key press. Keys are the names produced by parseKeys: "up",
// "down", "pgup", "pgdn", "home", "end", "enter", "esc", "tab", "backspace",
// "ctrl-c", or a single printable character.
func (m *Model) Key(key string) {
	if m.filing {
		m.filterKey(key)
		return
	}
	if m.detail && m.detailKey(key) {
		return
	}
	switch key {
	case "q", "ctrl-c":
		m.quitting = true
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.page)
	case "pgdn":
		m.moveCursor(m.page)
	case "home", "g":
		m.cursor = 0
		m.clampWindow()
	case "end", "G":
		m.cursor = len(m.visible) - 1
		m.clampCursor()
		m.clampWindow()
	case "1":
		m.view = ViewLog
	case "2":
		m.view = ViewHuman
	case "enter":
		if len(m.visible) > 0 {
			m.detail = true
			m.detailLine = 0
		}
	case "esc":
		switch {
		case m.detail:
			m.detail = false
		case !m.filter.Empty():
			m.filter = filter.Filter{}
			m.filterSpec = ""
			m.recompute()
		}
	case "tab":
		if m.detail {
			m.level = nextLevel(m.level)
			m.clampDetail()
		}
	case "/":
		m.filing = true
		m.editBuf = m.filterSpec
		m.status = ""
	default:
		if len(key) == 1 {
			m.status = "" // a stray printable key is not a command
		}
	}
}

// detailKey handles the navigation keys while the detail pane is open: they
// scroll the pane instead of moving the selection underneath it. It reports
// whether it consumed the key.
func (m *Model) detailKey(key string) bool {
	switch key {
	case "up", "k":
		m.scrollDetail(-1)
	case "down", "j":
		m.scrollDetail(1)
	case "pgup":
		m.scrollDetail(-m.detailRows())
	case "pgdn":
		m.scrollDetail(m.detailRows())
	case "home", "g":
		m.detailLine = 0
	case "end", "G":
		m.detailLine = m.maxDetailOffset()
	default:
		return false
	}
	return true
}

func (m *Model) moveCursor(delta int) {
	m.cursor += delta
	m.clampCursor()
	m.clampWindow()
}

func (m *Model) scrollDetail(delta int) {
	m.detailLine += delta
	m.clampDetail()
}

// detailRows is the number of body rows of the detail pane: the header and the
// status bar take two lines of the terminal.
func (m *Model) detailRows() int { return detailRows(m.h) }

// maxDetailOffset is the largest scroll offset that still fills the pane.
func (m *Model) maxDetailOffset() int {
	if !m.detail || len(m.visible) == 0 {
		return 0
	}
	n := len(detailBody(m.Visible(m.cursor), m.level, m.w)) - m.detailRows()
	if n < 0 {
		return 0
	}
	return n
}

func (m *Model) clampDetail() {
	if max := m.maxDetailOffset(); m.detailLine > max {
		m.detailLine = max
	}
	if m.detailLine < 0 {
		m.detailLine = 0
	}
}

func (m *Model) filterKey(key string) {
	switch key {
	case "enter":
		spec := strings.TrimSpace(m.editBuf)
		f, err := filter.ParseCategories(spec)
		if err != nil {
			m.status = err.Error()
			return // stay editing; the previous filter keeps applying
		}
		m.filter, m.filterSpec = f, spec
		m.filing = false
		m.status = ""
		m.recompute()
	case "esc":
		m.filing = false
		m.editBuf = ""
		m.status = ""
	case "backspace":
		if r := []rune(m.editBuf); len(r) > 0 {
			m.editBuf = string(r[:len(r)-1])
		}
	default:
		if len(key) == 1 && key[0] >= 32 && key[0] < 127 {
			m.editBuf += key
		}
	}
}

// nextLevel cycles through the three explanation depths.
func nextLevel(l knowledge.Level) knowledge.Level {
	switch l {
	case knowledge.LevelOverview:
		return knowledge.LevelTechnical
	case knowledge.LevelTechnical:
		return knowledge.LevelInDepth
	default:
		return knowledge.LevelOverview
	}
}

// requestRedraw asks the run loop for a redraw.
func (m *Model) requestRedraw() {
	if m.waker != nil {
		m.waker()
	}
}
