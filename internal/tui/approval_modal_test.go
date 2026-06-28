package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/permissions"
)

func sampleRequest() *permissions.Request {
	return &permissions.Request{Mode: permissions.ModeDev, Command: "rm -rf /"}
}

func TestApproval_NewApprovalModel(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	assert.NotNil(t, m)
	assert.Equal(t, "rm -rf /", m.Request.Command)
	assert.False(t, m.FocusedReason)
	assert.Empty(t, m.ReasonDraft)
}

func TestApproval_Init(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	assert.Nil(t, m.Init())
}

func TestApproval_SetSize(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.SetSize(5, 1)
	assert.GreaterOrEqual(t, m.Width, 30)
	assert.GreaterOrEqual(t, m.Height, 6)
}

func TestApproval_HandleKey_EnterOnce(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyEnter})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictAllowOnce, v)
}

func TestApproval_HandleKey_EnterInReasonMode(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.ReasonDraft = "needed for cleanup"
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyEnter})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictAllowSession, v)
}

func TestApproval_HandleKey_Esc(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyEsc})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictDeny, v)
}

func TestApproval_HandleKey_CtrlC(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyCtrlC})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictDeny, v)
}

func TestApproval_HandleKey_Y(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("y")})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictAllowOnce, v)
}

func TestApproval_HandleKey_N(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("n")})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictDeny, v)
}

func TestApproval_HandleKey_A(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("a")})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictAllowSession, v)
}

func TestApproval_HandleKey_R(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	_, close := m.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("r")})
	assert.False(t, close)
	assert.True(t, m.FocusedReason)
}

func TestApproval_HandleKey_CapitalLetters(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v, close := m.HandleKey(KeyEvent{Type: tea.KeyRunes, Runes: []rune("Y")})
	assert.True(t, close)
	assert.Equal(t, permissions.VerdictAllowOnce, v)
}

func TestApproval_HandleKey_TabToggle(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.HandleKey(KeyEvent{Type: tea.KeyTab})
	assert.True(t, m.FocusedReason)
	m.HandleKey(KeyEvent{Type: tea.KeyTab})
	assert.False(t, m.FocusedReason)
}

func TestApproval_HandleKey_Backspace(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.ReasonDraft = "hello"
	m.HandleKey(KeyEvent{Type: tea.KeyBackspace})
	assert.Equal(t, "hell", m.ReasonDraft)
}

func TestApproval_HandleKey_BackspaceAtZero(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.HandleKey(KeyEvent{Type: tea.KeyBackspace})
	assert.Empty(t, m.ReasonDraft)
}

func TestApproval_HandleKey_SpaceInReason(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.HandleKey(KeyEvent{Type: tea.KeySpace})
	assert.Equal(t, " ", m.ReasonDraft)
}

func TestApproval_HandleKey_SpaceIgnoredOutsideReason(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	_, close := m.HandleKey(KeyEvent{Type: tea.KeySpace})
	assert.False(t, close)
	assert.Empty(t, m.ReasonDraft)
}

func TestApproval_HandleKey_OtherRunesInReason(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.HandleKey(KeyEvent{Type: tea.KeyType(99), Runes: []rune("hello")})
	assert.Equal(t, "hello", m.ReasonDraft)
}

func TestApproval_HandleKey_OtherRunesIgnoredOutsideReason(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.HandleKey(KeyEvent{Type: tea.KeyType(99), Runes: []rune("x")})
	assert.Empty(t, m.ReasonDraft)
}

func TestApproval_Update_WindowSize(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	mod, cmd := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.NotNil(t, mod)
	assert.Nil(t, cmd)
	assert.Equal(t, 80, m.Width)
}

func TestApproval_Update_EnterEmitsMsg(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	require.NotNil(t, cmd)
	msg := cmd().(ApprovalMsg)
	assert.True(t, msg.Allow)
	assert.Equal(t, permissions.VerdictAllowOnce, msg.Verdict)
	assert.Equal(t, "rm -rf /", msg.Command)
}

func TestApproval_Update_EscEmitsDeny(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	require.NotNil(t, cmd)
	msg := cmd().(ApprovalMsg)
	assert.False(t, msg.Allow)
}

func TestApproval_Update_OtherKeyNoClose(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	assert.Nil(t, cmd)
	assert.True(t, m.FocusedReason)
}

func TestApproval_Update_IgnoresUnknownMsg(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	mod, cmd := m.Update(struct{ x int }{1})
	assert.NotNil(t, mod)
	assert.Nil(t, cmd)
}

func TestApproval_View_ContainsCommand(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v := m.View()
	assert.Contains(t, v, "rm -rf")
	assert.Contains(t, v, "dev")
}

func TestApproval_View_ReasonModePrompt(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	m.FocusedReason = true
	m.ReasonDraft = "needed"
	v := m.View()
	assert.Contains(t, v, "needed")
}

func TestApproval_View_ShortHint(t *testing.T) {
	m := NewApprovalModel(sampleRequest())
	v := m.View()
	assert.Contains(t, v, "y")
	assert.True(t, strings.Contains(v, "n"))
}