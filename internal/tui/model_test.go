package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/commands"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/store"
)

// newTestModel builds a Model wired to a real store.Conversation so the
// Append/Clear/Messages paths exercise the SQLite-backed persistence.
func newTestModel(t *testing.T) *Model {
	t.Helper()
	conv, err := store.Open(t.TempDir() + "/conv.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conv.Close() })

	m := NewModel(nil, mode.Universal{}, conv)
	m.width = 80
	m.height = 24
	m.ready = true
	return m
}

func TestNewModel(t *testing.T) {
	m := newTestModel(t)
	assert.NotNil(t, m)
	assert.False(t, m.streaming)
	assert.Equal(t, 0, m.ctxCount)
	assert.Nil(t, m.Registry())
}

func TestModel_Init(t *testing.T) {
	m := newTestModel(t)
	cmd := m.Init()
	assert.NotNil(t, cmd, "Init should return textinput.Blink")
}

func TestModel_SetContextCount(t *testing.T) {
	m := newTestModel(t)
	m.SetContextCount(3)
	assert.Equal(t, 3, m.ctxCount)
}

func TestModel_SetRegistry(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x", Name: "x"}}, nil)
	m.SetRegistry(r)
	assert.Same(t, r, m.Registry())
}

func TestModel_Update_WindowSize(t *testing.T) {
	m := newTestModel(t)
	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.Equal(t, 100, m.width)
	assert.Equal(t, 30, m.height)
	assert.True(t, m.ready)
}

func TestModel_Update_CtrlCQuits(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	assert.NotNil(t, cmd, "Ctrl+C should return tea.Quit")
}

func TestModel_Update_EscQuits(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.NotNil(t, cmd)
}

func TestModel_HandleSubmit_Empty(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("   ")
	assert.Nil(t, m.handleSubmit())
}

func TestModel_HandleSubmit_TextMessage(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("hello world")
	cmd := m.handleSubmit()
	assert.NotNil(t, cmd, "should start streaming")
	assert.True(t, m.streaming)
	assert.Empty(t, m.input.Value(), "input cleared after submit")
}

func TestModel_HandleSlash_Help(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/help")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "help")
}

func TestModel_HandleSlash_Clear(t *testing.T) {
	m := newTestModel(t)
	// Pre-populate conversation.
	m.input.SetValue("hello")
	_ = m.handleSubmit()

	m.input.SetValue("/clear")
	_ = m.handleSubmit()
	assert.Empty(t, m.messages, "messages cleared")
}

func TestModel_HandleSlash_Quit(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/quit")
	cmd := m.handleSubmit()
	assert.NotNil(t, cmd)
}

func TestModel_HandleSlash_ModeShow(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/mode")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "current mode")
}

func TestModel_HandleSlash_ModeSwitch(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/mode dev")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "mode → dev")
}

func TestModel_HandleSlash_ModeSwitchError(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/mode nonexistent")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "unknown mode")
}

func TestModel_HandleSlash_CommandsList(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x", Name: "x", Description: "X", Prompt: "p", Source: commands.SourceGlobal}}, nil)
	m.SetRegistry(r)

	m.input.SetValue("/commands list")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "1 commands")
}

func TestModel_HandleSlash_CommandsReload(t *testing.T) {
	m := newTestModel(t)
	m.SetRegistry(commands.NewRegistry())

	m.input.SetValue("/commands reload")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "reloaded")
}

func TestModel_HandleSlash_CommandsUnknownSub(t *testing.T) {
	m := newTestModel(t)
	m.SetRegistry(commands.NewRegistry())

	m.input.SetValue("/commands foo")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "unknown /commands subcommand")
}

func TestModel_HandleSlash_CommandsNoRegistry(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/commands list")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "no registry attached")
}

func TestModel_HandleSlash_CustomCommand(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{
		{Slash: "/hello", Name: "hello", Description: "hi", Prompt: "echo {{args}}", Source: commands.SourceGlobal},
	}, nil)
	m.SetRegistry(r)

	m.input.SetValue("/hello world")
	cmd := m.handleSubmit()
	assert.NotNil(t, cmd)
	// The expanded prompt ("echo world") was the last user message.
	assert.NotEmpty(t, m.messages)
	last := m.messages[len(m.messages)-2] // assistant placeholder is last
	assert.Equal(t, "user", last.Role)
	assert.Equal(t, "echo world", last.Content)
}

func TestModel_HandleSlash_UnknownCommand(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/unknown")
	_ = m.handleSubmit()
	assert.Contains(t, m.statusMsg, "unknown command")
}

func TestModel_OpenPicker_NoRegistry(t *testing.T) {
	m := newTestModel(t)
	m.openPicker()
	assert.Nil(t, m.picker)
	assert.Contains(t, m.statusMsg, "no command registry")
}

func TestModel_OpenPicker_WithRegistry(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x", Name: "x", Description: "x", Prompt: "x"}}, nil)
	m.SetRegistry(r)

	m.input.SetValue("/x")
	m.openPicker()
	require.NotNil(t, m.picker)
	assert.Equal(t, "x", m.picker.Query)
}

func TestModel_OpenPicker_WithQuery(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello", Name: "hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/hel")
	m.openPicker()
	require.NotNil(t, m.picker)
	assert.Equal(t, "hel", m.picker.Query)
}

func TestModel_PickerIntercept_EnterSelects(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{
		{Slash: "/hello", Name: "hello", Description: "hi", Prompt: "echo {{args}}"},
	}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	assert.Nil(t, cmd, "model consumes selection in-place")
	assert.Nil(t, m.picker, "picker must close after selection")
	assert.Equal(t, "/hello ", m.input.Value(), "selected slash token restored to input")
}

func TestModel_PickerIntercept_EscCancels(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	assert.Nil(t, cmd)
	assert.Nil(t, m.picker)
	assert.Equal(t, "/", m.input.Value(), "input unchanged on cancel")
	assert.Contains(t, m.statusMsg, "cancelled")
}

func TestModel_PickerIntercept_RoutesOtherKeys(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{
		{Slash: "/hello"}, {Slash: "/world"}, {Slash: "/foo"},
	}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Nil(t, cmd, "down arrow should not emit a command")
	require.NotNil(t, m.picker)
	assert.Equal(t, 1, m.picker.Cursor)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.picker.Cursor)

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	assert.Equal(t, 2, m.picker.Cursor, "must clamp at the end")

	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	assert.Equal(t, 1, m.picker.Cursor)
}

func TestModel_PickerIntercept_WindowSize(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	assert.Equal(t, 100, m.picker.Width)
	assert.Equal(t, 100, m.width)
}

func TestModel_Update_TabOpensPicker(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	require.NotNil(t, m.picker, "Tab on / input must open the picker")
}

func TestModel_Update_TabIgnoredWithoutSlash(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("plain text")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Nil(t, m.picker)
}

func TestModel_Update_TabIgnoredWithoutRegistry(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/hello")
	_, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Nil(t, m.picker)
}

func TestModel_RefreshViewport(t *testing.T) {
	m := newTestModel(t)
	m.messages = []store.Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	}
	m.refreshViewport()
	assert.NotEmpty(t, m.viewport.View())
}

func TestModel_RefreshViewport_AllRoles(t *testing.T) {
	m := newTestModel(t)
	m.messages = []store.Message{
		{Role: "user", Content: "u"},
		{Role: "assistant", Content: "a"},
		{Role: "system", Content: "s"},
	}
	m.refreshViewport()
	v := m.viewport.View()
	assert.Contains(t, v, "u")
	assert.Contains(t, v, "a")
	assert.Contains(t, v, "s")
}

func TestModel_View_NotReady(t *testing.T) {
	m := newTestModel(t)
	m.ready = false
	v := m.View()
	assert.Equal(t, "initializing jito...", v)
}

func TestModel_View_Ready(t *testing.T) {
	m := newTestModel(t)
	v := m.View()
	assert.Contains(t, v, "jito")
}

func TestModel_View_WithPickerOverlay(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	v := m.View()
	assert.Contains(t, v, "slash commands")
}

func TestModel_FooterLine_NoContext(t *testing.T) {
	m := newTestModel(t)
	assert.Empty(t, m.footerLine())
}

func TestModel_FooterLine_OneContext(t *testing.T) {
	m := newTestModel(t)
	m.SetContextCount(1)
	assert.Contains(t, m.footerLine(), "1 context file loaded")
}

func TestModel_FooterLine_ManyContext(t *testing.T) {
	m := newTestModel(t)
	m.SetContextCount(5)
	assert.Contains(t, m.footerLine(), "5 context files loaded")
}

func TestModel_StreamChunkMsg(t *testing.T) {
	m := newTestModel(t)
	m.messages = append(m.messages, store.Message{Role: "assistant", Content: ""})
	_, cmd := m.Update(streamChunkMsg("hi"))
	assert.NotNil(t, cmd)
	assert.Equal(t, "hi", m.messages[len(m.messages)-1].Content)
}

func TestModel_StreamChunkMsg_EmptyMessages(t *testing.T) {
	m := newTestModel(t)
	_, cmd := m.Update(streamChunkMsg("hi"))
	assert.NotNil(t, cmd)
}

func TestModel_StreamDoneMsg(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	m.messages = append(m.messages, store.Message{Role: "assistant", Content: "done"})
	_, _ = m.Update(streamDoneMsg{})
	assert.False(t, m.streaming)
}

func TestModel_StreamErrMsg(t *testing.T) {
	m := newTestModel(t)
	m.streaming = true
	_, _ = m.Update(streamErrMsg{err: errStub})
	assert.False(t, m.streaming)
	assert.NotNil(t, m.err)
}

type stubErr struct{ s string }

func (e stubErr) Error() string { return e.s }

var errStub = stubErr{s: "boom"}

func TestHelpText_NoRegistry(t *testing.T) {
	h := helpText(nil)
	assert.Contains(t, h, "/help")
}

func TestHelpText_WithRegistry(t *testing.T) {
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/x", Name: "x", Description: "x", Prompt: "x", Source: commands.SourceGlobal}}, nil)
	h := helpText(r)
	assert.Contains(t, h, "custom commands")
	assert.Contains(t, h, "/x")
}

func TestModel_PickerMsg_RestoresInput(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, _ = m.Update(PickerMsg{Selected: "/hello"})
	assert.Equal(t, "/hello ", m.input.Value())
}

func TestModel_PickerMsg_Cancelled(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, _ = m.Update(PickerMsg{Selected: ""})
	assert.Contains(t, m.statusMsg, "picker cancelled")
}

func TestModel_PickerIntercept_OtherMsg(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()

	_, cmd := m.Update(struct{ x int }{1})
	assert.Nil(t, cmd)
	require.NotNil(t, m.picker)
}

func TestModel_PickerOverlay_ShownInView(t *testing.T) {
	m := newTestModel(t)
	r := commands.NewRegistry()
	r.Update([]*commands.Command{{Slash: "/hello", Name: "hello", Description: "hi", Prompt: "echo {{args}}"}}, nil)
	m.SetRegistry(r)
	m.input.SetValue("/")
	m.openPicker()
	v := m.View()
	assert.True(t, strings.Contains(v, "hello"), "picker must overlay with content")
}