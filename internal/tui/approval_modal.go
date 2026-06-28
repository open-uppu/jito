package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/uppu/jito/internal/permissions"
)

// ApprovalMsg is sent when the user has resolved an approval prompt.
// Allow is true when execution may proceed.
type ApprovalMsg struct {
	Allow     bool
	Reason    string
	Verdict   permissions.Verdict
	Command   string
}

// ApprovalModel is the headless state of the y/n approval modal.
// Decoupled from Bubble Tea Msg so unit tests can drive it directly.
type ApprovalModel struct {
	Request *permissions.Request
	Width   int
	Height  int
	// ReasonDraft is the user's free-text justification while typing.
	ReasonDraft string
	// FocusedReason is true when the user is editing the reason field;
	// false means only y/n keys are accepted.
	FocusedReason bool

	TitleStyle lipgloss.Style
	BodyStyle  lipgloss.Style
	KeyStyle   lipgloss.Style
	HintStyle  lipgloss.Style
}

// NewApprovalModel builds a modal for the given request.
func NewApprovalModel(req *permissions.Request) *ApprovalModel {
	return &ApprovalModel{
		Request:      req,
		Width:        60,
		Height:       10,
		TitleStyle:   titleStyle,
		BodyStyle:    lipgloss.NewStyle().Padding(0, 1),
		KeyStyle:     lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FF5F87")),
		HintStyle:    statusStyle,
		FocusedReason: false,
	}
}

// SetSize updates the modal dimensions.
func (a *ApprovalModel) SetSize(w, h int) {
	if w < 30 {
		w = 30
	}
	if h < 6 {
		h = 6
	}
	a.Width = w
	a.Height = h
}

// --- Headless message handlers ---

// HandleKey applies a keystroke and returns (verdict, close).  When
// close is true the caller must emit an ApprovalMsg and pop the modal.
func (a *ApprovalModel) HandleKey(k KeyEvent) (verdict permissions.Verdict, close bool) {
	switch k.Type {
	case tea.KeyEsc, tea.KeyCtrlC:
		return permissions.VerdictDeny, true
	case tea.KeyEnter:
		if a.FocusedReason {
			// Commit the reason and approve-session.
			return permissions.VerdictAllowSession, true
		}
		return permissions.VerdictAllowOnce, true
	case tea.KeyRunes:
		rs := string(k.Runes)
		if !a.FocusedReason {
			switch strings.ToLower(rs) {
			case "y":
				return permissions.VerdictAllowOnce, true
			case "n":
				return permissions.VerdictDeny, true
			case "a":
				return permissions.VerdictAllowSession, true
			case "r":
				a.FocusedReason = true
			}
		} else {
			a.ReasonDraft += rs
		}
	case tea.KeySpace:
		if a.FocusedReason {
			a.ReasonDraft += " "
		}
	case tea.KeyBackspace:
		if a.FocusedReason && len(a.ReasonDraft) > 0 {
			r := []rune(a.ReasonDraft)
			a.ReasonDraft = string(r[:len(r)-1])
		}
	case tea.KeyTab:
		// Toggle reason focus.
		a.FocusedReason = !a.FocusedReason
	default:
		if len(k.Runes) > 0 && a.FocusedReason {
			a.ReasonDraft += string(k.Runes)
		}
	}
	return 0, false
}

// --- Bubble Tea wiring ---

// Init is a no-op.
func (a *ApprovalModel) Init() tea.Cmd { return nil }

// Update is the Bubble Tea message handler.
func (a *ApprovalModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.SetSize(m.Width, m.Height)
		return a, nil
	case tea.KeyMsg:
		ev := KeyEvent{Type: m.Type, Runes: m.Runes}
		v, close := a.HandleKey(ev)
		if close {
			reason := a.ReasonDraft
			return a, func() tea.Msg {
				return ApprovalMsg{
					Allow:   v != permissions.VerdictDeny,
					Reason:  reason,
					Verdict: v,
					Command: a.Request.Command,
				}
			}
		}
	}
	return a, nil
}

// View renders the modal.
func (a *ApprovalModel) View() string {
	title := a.TitleStyle.Render("⚠ permission required")
	cmdLine := a.BodyStyle.Render(fmt.Sprintf("mode:   %s", a.Request.Mode))
	cmdLine2 := a.BodyStyle.Render(fmt.Sprintf("shell:  %s", truncate(a.Request.Command, a.Width-10)))
	allowlist := permissions.FormatAllowlist(a.Request.Mode,
		permissions.DefaultAllowlist(a.Request.Mode))
	allowlistLine := a.BodyStyle.Render(truncate(allowlist, a.Width-4))

	var promptLine string
	if a.FocusedReason {
		promptLine = a.HintStyle.Render(fmt.Sprintf("reason: %s▌  (Enter to allow-session, Esc to deny, Tab to toggle)", a.ReasonDraft))
	} else {
		promptLine = a.HintStyle.Render("y allow once · a allow for session · n deny · r reason")
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		title, cmdLine, cmdLine2, allowlistLine, promptLine)
}