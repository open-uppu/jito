// Package tui implements the Bubble Tea terminal UI for jito chat.
package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/store"
)

// Styles (Lip Gloss)
var (
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true).
			Padding(0, 1)

	userStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#04B575")).
			Bold(true)

	agentStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF5F87")).
			Bold(true)

	statusStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888")).
			Italic(true)
)

// Model is the Bubble Tea program state.
type Model struct {
	viewport  viewport.Model
	input     textinput.Model
	conv      *store.Conversation
	provider  provider.Provider
	mode      mode.Mode
	messages  []store.Message
	width     int
	height    int
	ready     bool
	streaming bool
	err       error
	statusMsg string
}

// NewModel constructs a new TUI model.
func NewModel(p provider.Provider, m mode.Mode, conv *store.Conversation) *Model {
	ti := textinput.New()
	ti.Placeholder = "ask jito... (try /help)"
	ti.Focus()
	ti.CharLimit = 4096
	ti.Width = 80

	vp := viewport.New(80, 20)
	vp.SetContent("")

	return &Model{
		viewport: vp,
		input:    ti,
		conv:     conv,
		provider: p,
		mode:     m,
		messages: conv.Messages(),
	}
}

// Init is the initial Bubble Tea command.
func (m *Model) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 3 // minus input + status
		m.input.Width = msg.Width - 2
		m.ready = true

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			if !m.streaming && m.input.Value() != "" {
				return m, m.handleSubmit()
			}
		}

	case streamChunkMsg:
		// Append chunk to the last assistant message
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			m.messages[len(m.messages)-1].Content += string(msg)
			m.refreshViewport()
		}
		return m, m.waitForChunk()

	case streamDoneMsg:
		m.streaming = false
		m.statusMsg = fmt.Sprintf("[%s mode]", m.mode.Name())
		// Persist final assistant message
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].Role == "assistant" {
			_ = m.conv.Append(m.messages[len(m.messages)-1])
		}
		m.refreshViewport()
		return m, nil

	case streamErrMsg:
		m.streaming = false
		m.err = msg.err
		m.statusMsg = errorStyle.Render(fmt.Sprintf("error: %v", msg.err))
		m.refreshViewport()
		return m, nil
	}

	m.input, tiCmd = m.input.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(tiCmd, vpCmd)
}

// View renders the TUI.
func (m *Model) View() string {
	if !m.ready {
		return "initializing jito..."
	}
	header := titleStyle.Render(fmt.Sprintf("⚡ jito · %s", m.mode.Name()))
	body := m.viewport.View()
	input := fmt.Sprintf("> %s", m.input.View())
	status := statusStyle.Render(m.statusMsg)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, input, status)
}

// --- Internal helpers ---

func (m *Model) handleSubmit() tea.Cmd {
	text := strings.TrimSpace(m.input.Value())
	if text == "" {
		return nil
	}

	// Slash command?
	if strings.HasPrefix(text, "/") {
		return m.handleSlash(text)
	}

	// Append user message
	um := store.Message{Role: "user", Content: text, Mode: m.mode.Name()}
	m.messages = append(m.messages, um)
	_ = m.conv.Append(um)

	// Append empty assistant message (will be filled by stream)
	am := store.Message{Role: "assistant", Content: "", Mode: m.mode.Name()}
	m.messages = append(m.messages, am)

	m.input.Reset()
	m.streaming = true
	m.statusMsg = "thinking..."

	return m.startStream()
}

func (m *Model) refreshViewport() {
	var sb strings.Builder
	for _, msg := range m.messages {
		switch msg.Role {
		case "user":
			sb.WriteString(userStyle.Render("you › ") + msg.Content + "\n\n")
		case "assistant":
			sb.WriteString(agentStyle.Render("jito › ") + msg.Content + "\n\n")
		case "system":
			sb.WriteString(statusStyle.Render(msg.Content) + "\n")
		}
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m *Model) handleSlash(text string) tea.Cmd {
	parts := strings.Fields(text)
	cmd := parts[0]
	switch cmd {
	case "/help":
		helpMsg := store.Message{Role: "system", Content: helpText(), Mode: m.mode.Name()}
		m.messages = append(m.messages, helpMsg)
		_ = m.conv.Append(helpMsg)
		m.statusMsg = "loaded help"
	case "/clear":
		m.messages = nil
		m.conv.Clear()
		m.statusMsg = "conversation cleared"
	case "/mode":
		if len(parts) > 1 {
			newMode, err := mode.Get(parts[1])
			if err == nil {
				m.mode = newMode
				m.statusMsg = fmt.Sprintf("mode → %s", newMode.Name())
			} else {
				m.statusMsg = errorStyle.Render(err.Error())
			}
		} else {
			m.statusMsg = fmt.Sprintf("current mode: %s", m.mode.Name())
		}
	case "/quit", "/exit":
		return tea.Quit
	default:
		m.statusMsg = errorStyle.Render(fmt.Sprintf("unknown command: %s (try /help)", cmd))
	}
	m.refreshViewport()
	return nil
}

func helpText() string {
	return `slash commands:
  /help              show this help
  /clear             clear conversation history
  /mode [name]       switch mode (dev|reason|create|audit|universal)
  /quit, /exit       exit jito

key bindings:
  Ctrl+C, Esc        quit
  Enter              send message`
}

// --- Streaming ---

type streamChunkMsg string
type streamDoneMsg struct{}
type streamErrMsg struct{ err error }

func (m *Model) startStream() tea.Cmd {
	return tea.Batch(
		m.streamChat(),
		m.waitForChunk(),
	)
}

func (m *Model) waitForChunk() tea.Cmd {
	return func() tea.Msg {
		// This is a no-op; real chunks come from streamChat goroutine
		return nil
	}
}

func (m *Model) streamChat() tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()
		systemPrompt := m.mode.SystemPrompt()

		// Build full conversation
		var convo strings.Builder
		convo.WriteString(systemPrompt + "\n\n")
		for _, msg := range m.messages[:len(m.messages)-1] { // exclude empty assistant
			convo.WriteString(fmt.Sprintf("%s: %s\n", msg.Role, msg.Content))
		}

		// Stream chunks
		err := m.provider.StreamChat(ctx, systemPrompt, convo.String(), func(chunk string) error {
			// Bubble Tea programs need to send messages via Program, not return
			// For now, use simple non-streaming path
			return nil
		})
		if err != nil {
			return streamErrMsg{err: err}
		}
		return streamDoneMsg{}
	}
}