package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/commands"
)

func sampleCommands() []*commands.Command {
	return []*commands.Command{
		{Slash: "/hello", Name: "hello", Description: "say hello", Prompt: "echo {{args}}", Source: commands.SourceGlobal},
		{Slash: "/help", Name: "help", Description: "show help", Prompt: "help", Source: commands.SourceBuiltIn},
		{Slash: "/git:commit", Name: "git:commit", Description: "git commit", Prompt: "git {{args}}", Source: commands.SourceGlobal},
		{Slash: "/git:status", Name: "git:status", Description: "git status", Prompt: "git st", Source: commands.SourceGlobal},
		{Slash: "/lint", Name: "lint", Description: "lint code", Prompt: "lint", Source: commands.SourceProject},
	}
}

func TestPicker_InitialState(t *testing.T) {
	p := NewPicker()
	assert.NotNil(t, p)
	assert.Equal(t, 0, p.Cursor)
	assert.Empty(t, p.Items)
}

func TestPicker_Init(t *testing.T) {
	p := NewPicker()
	assert.Nil(t, p.Init())
}

func TestPicker_SetCommands_AllShown(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	assert.Len(t, p.Items, 5)
	assert.Len(t, p.snapshot, 5, "snapshot must hold the same number of commands")
}

func TestPicker_SetCommands_Empty(t *testing.T) {
	p := NewPicker()
	p.SetCommands(nil)
	assert.Empty(t, p.Items)
}

func TestPicker_SetCommands_PreservesQuery(t *testing.T) {
	p := NewPicker()
	p.Query = "git"
	p.SetCommands(sampleCommands())
	assert.Len(t, p.Items, 2, "only git:* matches the query")
}

func TestPicker_Filter_Prefix(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.Query = "git"
	// refresh after setting query
	p.applyFilter(p.snapshot, p.Query)
	assert.Len(t, p.Items, 2)
}

func TestPicker_Filter_NoMatch(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.Query = "xyz"
	p.applyFilter(p.snapshot, p.Query)
	assert.Empty(t, p.Items)
}

func TestPicker_HandleKey_DownUp(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())

	p.HandleKey(KeyEvent{Type: tea.KeyDown})
	assert.Equal(t, 1, p.Cursor)
	p.HandleKey(KeyEvent{Type: tea.KeyDown})
	assert.Equal(t, 2, p.Cursor)
	p.HandleKey(KeyEvent{Type: tea.KeyUp})
	assert.Equal(t, 1, p.Cursor)
}

func TestPicker_HandleKey_DownClamps(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	for i := 0; i < 100; i++ {
		p.HandleKey(KeyEvent{Type: tea.KeyDown})
	}
	assert.Equal(t, len(p.Items)-1, p.Cursor)
}

func TestPicker_HandleKey_UpClamps(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.HandleKey(KeyEvent{Type: tea.KeyUp})
	assert.Equal(t, 0, p.Cursor)
}

func TestPicker_HandleKey_Backspace(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.Query = "git"
	close := p.HandleKey(KeyEvent{Type: tea.KeyBackspace})
	assert.False(t, close)
	assert.Equal(t, "gi", p.Query)
}

func TestPicker_HandleKey_Runes(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("h")})
	p.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("e")})
	p.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("l")})
	assert.Equal(t, "hel", p.Query)
}

func TestPicker_HandleKey_EscCloses(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	close := p.HandleKey(KeyEvent{Type: tea.KeyEsc})
	assert.True(t, close)
}

func TestPicker_HandleKey_EnterCloses(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	close := p.HandleKey(KeyEvent{Type: tea.KeyEnter})
	assert.True(t, close)
	assert.NotNil(t, p.Selected())
}

func TestPicker_HandleKey_Space(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	close := p.HandleKey(KeyEvent{Type: tea.KeySpace})
	assert.False(t, close)
	assert.Equal(t, " ", p.Query)
}

func TestPicker_HandleKey_OtherKey_AppendsToQuery(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	close := p.HandleKey(KeyEvent{Type: tea.KeyType(0), Runes: []rune("x")})
	assert.False(t, close)
	assert.Equal(t, "x", p.Query)
}

func TestPicker_Selected_Empty(t *testing.T) {
	p := NewPicker()
	assert.Nil(t, p.Selected())
}

func TestPicker_SetSize_Floor(t *testing.T) {
	p := NewPicker()
	p.SetSize(5, 2)
	assert.GreaterOrEqual(t, p.Width, 20)
	assert.GreaterOrEqual(t, p.Height, 5)
}

func TestPicker_VisibleCount(t *testing.T) {
	p := NewPicker()
	p.SetSize(80, 24)
	assert.Equal(t, 21, p.VisibleCount())
	p.SetSize(80, 3)
	assert.GreaterOrEqual(t, p.VisibleCount(), 0)
}

func TestPicker_VisibleCount_Small(t *testing.T) {
	p := NewPicker()
	p.SetSize(80, 2)
	v := p.VisibleCount()
	assert.GreaterOrEqual(t, v, 0)
}

func TestPicker_View_RendersTitle(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	v := p.View()
	assert.Contains(t, v, "slash commands")
	assert.Contains(t, v, "/hello")
}

func TestPicker_View_EmptyMessage(t *testing.T) {
	p := NewPicker()
	v := p.View()
	assert.Contains(t, v, "no commands")
}

func TestPicker_View_ShowsCount(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	v := p.View()
	assert.Contains(t, v, "1/5")
}

func TestPicker_View_FilterQueryVisible(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	p.Query = "git"
	p.applyFilter(p.snapshot, p.Query)
	v := p.View()
	assert.Contains(t, v, "/git")
}

func TestPicker_Update_WindowSize(t *testing.T) {
	p := NewPicker()
	m, cmd := p.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.NotNil(t, m)
	assert.Nil(t, cmd)
	assert.Equal(t, 80, p.Width)
}

func TestPicker_Update_KeyMsg(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	m, cmd := p.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.NotNil(t, m)
	assert.Nil(t, cmd)
	assert.Equal(t, 1, p.Cursor)
}

func TestPicker_Update_EnterEmitsPickerMsg(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd()
	pm, ok := msg.(PickerMsg)
	require.True(t, ok)
	// Empty query → alphabetic sort; first is "/git:commit".
	assert.Equal(t, "/git:commit", pm.Selected)
}

func TestPicker_Update_EscEmitsPickerMsgEmpty(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	pm := cmd().(PickerMsg)
	assert.Empty(t, pm.Selected)
}

func TestPicker_Update_IgnoresUnknownMsg(t *testing.T) {
	p := NewPicker()
	p.SetCommands(sampleCommands())
	m, cmd := p.Update(struct{ foo int }{42})
	assert.NotNil(t, m)
	assert.Nil(t, cmd)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "abc", truncate("abc", 5))
	assert.Equal(t, "ab…", truncate("abcdef", 3))
	assert.Equal(t, "a", truncate("abc", 1))
}

func TestMatchIndices(t *testing.T) {
	pos, ok := matchIndices("/hello", "hel")
	assert.True(t, ok)
	assert.Equal(t, []int{1, 2, 3}, pos)

	_, ok = matchIndices("/hello", "xyz")
	assert.False(t, ok)

	_, ok = matchIndices("/hello", "")
	assert.True(t, ok)
}

func TestScoreHit(t *testing.T) {
	// /help (5 runes) has less length penalty than /hello (6 runes),
	// so /help scores higher when both are prefix matches.
	assert.Less(t, scoreHit("/hello", "hel", []int{1, 2, 3}), scoreHit("/help", "hel", []int{1, 2, 4}))
	assert.Equal(t, 0, scoreHit("/hello", "", nil))
}

func TestPicker_View_Scrolling(t *testing.T) {
	cmds := make([]*commands.Command, 30)
	for i := range cmds {
		cmds[i] = &commands.Command{Slash: "/cmd" + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)), Description: "d", Prompt: "p"}
	}
	p := NewPicker()
	p.SetSize(80, 10)
	p.SetCommands(cmds)
	for i := 0; i < 25; i++ {
		p.HandleKey(KeyEvent{Type: tea.KeyDown})
	}
	v := p.View()
	assert.True(t, strings.Contains(v, "/cmd"), "rendered rows must still mention commands")
}