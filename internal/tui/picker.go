package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/uppu/jito/internal/commands"
)

// PickerMsg is sent when the user has selected (or dismissed) the
// picker.  Selected is empty when the user cancelled (Esc / Ctrl-C).
type PickerMsg struct {
	Selected string // slash token, e.g. "/git:commit"; empty on cancel
	Args     string // trailing argument text after the slash token
}

// PickerModel is the headless state of the slash-command picker.  It is
// deliberately separated from any Bubble Tea Msg types so unit tests can
// drive it directly without spinning up a tea.Program.
//
// The picker keeps its own snapshot of the registry at construction
// time; reloading is the caller's responsibility (the TUI calls Reload
// when it receives a ReloadMsg).
type PickerModel struct {
	// Query is the current fuzzy-filter text (without leading slash).
	Query string
	// Cursor is the index of the highlighted row in Items.
	Cursor int
	// Items is the currently filtered list of commands, in display order.
	Items []*commands.Command
	// snapshot holds every command currently visible to the picker so
	// keystroke handlers can re-filter without external state.
	snapshot []*commands.Command
	// Width and Height bound the rendered modal.
	Width  int
	Height int
	// styles are injected so tests can supply a no-op style.
	TitleStyle lipgloss.Style
	ItemStyle  lipgloss.Style
	SelStyle   lipgloss.Style
	HintStyle  lipgloss.Style
}

// NewPicker constructs an empty picker.  Callers should immediately
// invoke SetRegistry so the picker has something to display.
func NewPicker() *PickerModel {
	return &PickerModel{
		Width:      60,
		Height:     12,
		TitleStyle: titleStyle,
		ItemStyle:  lipgloss.NewStyle().Padding(0, 1),
		SelStyle:   lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("#04B575")),
		HintStyle:  statusStyle,
	}
}

// SetSize updates the modal dimensions (called from WindowSizeMsg).
func (p *PickerModel) SetSize(w, h int) {
	if w < 20 {
		w = 20
	}
	if h < 5 {
		h = 5
	}
	p.Width = w
	p.Height = h
}

// SetRegistry replaces the underlying list.  It preserves the current
// Query and refreshes the filtered view.
func (p *PickerModel) SetRegistry(reg *commands.Registry) {
	p.SetCommands(reg.All())
}

// SetCommands replaces the underlying list.  Preserves the current
// Query and refreshes Items.
func (p *PickerModel) SetCommands(cmds []*commands.Command) {
	p.snapshot = cmds
	p.applyFilter(cmds, p.Query)
	if p.Cursor >= len(p.Items) {
		p.Cursor = max(0, len(p.Items)-1)
	}
}

// applyFilter is the headless filtering helper.  It exposes the same
// algorithm the Bubble Tea Update path uses so tests can drive it
// without constructing tea.Msg values.
func (p *PickerModel) applyFilter(cmds []*commands.Command, query string) {
	q := strings.TrimPrefix(query, "/")
	type ranked struct {
		cmd   *commands.Command
		score int
	}
	var rs []ranked
	for _, c := range cmds {
		pos, ok := matchIndices(c.Slash, q)
		_ = pos
		if q == "" {
			rs = append(rs, ranked{c, 0})
			continue
		}
		if ok {
			rs = append(rs, ranked{c, scoreHit(c.Slash, q, pos)})
		}
	}
	sort.SliceStable(rs, func(i, j int) bool {
		if rs[i].score != rs[j].score {
			return rs[i].score > rs[j].score
		}
		return rs[i].cmd.Slash < rs[j].cmd.Slash
	})
	p.Items = make([]*commands.Command, len(rs))
	for i, r := range rs {
		p.Items[i] = r.cmd
	}
}

// VisibleCount returns the number of rows that fit on screen.
func (p *PickerModel) VisibleCount() int {
	// 1 title + 1 hint + 1 footer
	if p.Height < 4 {
		return max(0, p.Height-2)
	}
	return p.Height - 3
}

// Selected returns the command under the cursor, or nil when empty.
func (p *PickerModel) Selected() *commands.Command {
	if p.Cursor < 0 || p.Cursor >= len(p.Items) {
		return nil
	}
	return p.Items[p.Cursor]
}

// --- Headless message handlers (pure functions, no tea.Msg) ---

// KeyEvent is a small portable subset of tea.KeyMsg used by the headless
// API.  The Bubble Tea Update method translates real tea.KeyMsg values
// into KeyEvent before delegating.
type KeyEvent struct {
	Type  tea.KeyType
	Runes []rune
}

// HandleKey applies a keystroke and returns the picker state plus a
// boolean indicating whether the picker wants to close (true → caller
// should issue a PickerMsg and remove the picker from the model stack).
func (p *PickerModel) HandleKey(k KeyEvent) (close bool) {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return true
	case tea.KeyEnter:
		return true
	case tea.KeyUp:
		if p.Cursor > 0 {
			p.Cursor--
		}
		return false
	case tea.KeyDown:
		if p.Cursor < len(p.Items)-1 {
			p.Cursor++
		}
		return false
	case tea.KeyBackspace:
		if len(p.Query) > 0 {
			r := []rune(p.Query)
			p.Query = string(r[:len(r)-1])
		}
	case tea.KeyRunes:
		p.Query += string(k.Runes)
	case tea.KeySpace:
		p.Query += " "
	default:
		// Treat other keys as raw runes (good enough for headless tests).
		if len(k.Runes) > 0 {
			p.Query += string(k.Runes)
		}
	}
	p.applyFilter(p.snapshot, p.Query)
	if p.Cursor >= len(p.Items) {
		p.Cursor = max(0, len(p.Items)-1)
	}
	return false
}

// snapshot is filled by SetCommands so HandleKey can refilter without
// the caller having to re-supply the registry on every keystroke.
// (Storage is the snapshot field on PickerModel.)

// --- Bubble Tea wiring ---

// Init is part of the tea.Model contract; the picker has nothing to do
// at startup.
func (p *PickerModel) Init() tea.Cmd { return nil }

// Update is the Bubble Tea message handler.  It dispatches keyboard
// input to HandleKey and returns a PickerMsg on selection/cancel.
func (p *PickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		p.SetSize(m.Width, m.Height)
		return p, nil
	case tea.KeyMsg:
		ev := KeyEvent{Type: m.Type, Runes: m.Runes}
		close := p.HandleKey(ev)
		if close {
			var sel string
			if m.Type == tea.KeyEnter {
				if c := p.Selected(); c != nil {
					sel = c.Slash
				}
			}
			return p, func() tea.Msg { return PickerMsg{Selected: sel} }
		}
	}
	return p, nil
}

// View renders the picker modal.
func (p *PickerModel) View() string {
	title := p.TitleStyle.Render("⚡ slash commands")
	hint := p.HintStyle.Render(fmt.Sprintf("filter: /%s   (↑↓ enter esc)", p.Query))

	if len(p.Items) == 0 {
		body := p.ItemStyle.Render("(no commands — run /commands reload)")
		return lipgloss.JoinVertical(lipgloss.Left, title, body, hint)
	}

	visible := p.VisibleCount()
	start := 0
	if p.Cursor >= visible {
		start = p.Cursor - visible + 1
	}
	end := start + visible
	if end > len(p.Items) {
		end = len(p.Items)
	}

	var rows []string
	for i := start; i < end; i++ {
		c := p.Items[i]
		marker := "  "
		style := p.ItemStyle
		if i == p.Cursor {
			marker = "› "
			style = p.SelStyle
		}
		line := fmt.Sprintf("%s%-22s %s", marker, truncate(c.Slash, 22), c.Description)
		rows = append(rows, style.Render(line))
	}

	footer := p.HintStyle.Render(fmt.Sprintf("%d/%d", p.Cursor+1, len(p.Items)))

	return lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n"), hint, footer)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// --- Fuzzy helpers shared with the registry (kept here so the picker
// can run even if the registry is nil) ---

func matchIndices(haystack, query string) ([]int, bool) {
	if query == "" {
		return nil, true
	}
	h := []rune(haystack)
	q := []rune(query)
	pos := make([]int, 0, len(q))
	qi := 0
	for hi := 0; hi < len(h) && qi < len(q); hi++ {
		if h[hi] == q[qi] {
			pos = append(pos, hi)
			qi++
		}
	}
	if qi < len(q) {
		return nil, false
	}
	return pos, true
}

func scoreHit(slash, query string, pos []int) int {
	if query == "" {
		return 0
	}
	score := 10 * len(pos)
	if len(pos) > 0 && pos[0] == 1 {
		score += 100
	}
	h := []rune(slash)
	score -= len(h) - len(pos)
	return score
}