// Package tui implements the Bubble Tea terminal UI for jito chat.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/uppu/jito/internal/commands"
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
	// ctxCount is the number of JITO.md context files loaded; shown in
	// the footer (gemini-cli analog). Zero means no loader attached.
	ctxCount int
	// registry holds custom slash commands (LOOP #2).  Nil disables the
	// slash picker; commands.IsBuiltin still work via the keyboard.
	registry *commands.Registry
	// picker is the active slash-command picker modal.  Non-nil only
	// while the picker is on screen.
	picker *PickerModel
	// pendingSlash is the raw text the user typed before opening the
	// picker; restored after selection so the input field can be
	// re-populated with the expanded prompt.
	pendingSlash string
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

// SetContextCount updates the context-file counter shown in the footer.
// Callers wire this after building the Loader.
func (m *Model) SetContextCount(n int) { m.ctxCount = n }

// SetRegistry attaches a custom-slash-command registry.  Once attached,
// typing a leading "/" in the input opens the picker modal.
func (m *Model) SetRegistry(r *commands.Registry) { m.registry = r }

// Registry returns the current custom-command registry (may be nil).
func (m *Model) Registry() *commands.Registry { return m.registry }

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	// Picker intercepts everything while it's on screen.
	if m.picker != nil {
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			m.picker.SetSize(msg.Width, msg.Height)
			m.width = msg.Width
			m.height = msg.Height
			return m, nil
		case tea.KeyMsg:
			close := m.picker.HandleKey(KeyEvent{Type: msg.Type, Runes: msg.Runes})
			if !close {
				return m, nil
			}
			var sel string
			if msg.Type == tea.KeyEnter {
				if c := m.picker.Selected(); c != nil {
					sel = c.Slash
				}
			}
			m.picker = nil
			if sel != "" {
				m.input.SetValue(sel + " ")
				m.input.CursorEnd()
				m.statusMsg = "command selected"
			} else {
				m.statusMsg = "picker cancelled"
			}
			return m, nil
		case PickerMsg:
			// External emission (defensive): consume and apply.
			m.picker = nil
			if msg.Selected != "" {
				m.input.SetValue(msg.Selected + " ")
				m.input.CursorEnd()
			} else {
				m.statusMsg = "picker cancelled"
			}
			return m, nil
		}
		return m, nil
	}

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
		case tea.KeyTab:
			// Tab is the picker hot-key when an attached registry
			// knows about the current input.
			if m.registry != nil && strings.HasPrefix(strings.TrimSpace(m.input.Value()), "/") {
				m.openPicker()
				return m, nil
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
	footer := m.footerLine()
	base := lipgloss.JoinVertical(lipgloss.Left, header, body, input, status, footer)
	if m.picker != nil {
		overlay := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(0, 1).
			Width(m.picker.Width).
			Render(m.picker.View())
		base += "\n" + overlay
	}
	return base
}

// footerLine returns the context-count footer (gemini-cli analog).
// Returns "" when ctxCount is zero (no loader attached) so the line is
// suppressed rather than showing "0 context files loaded".
func (m *Model) footerLine() string {
	if m.ctxCount <= 0 {
		return ""
	}
	if m.ctxCount == 1 {
		return statusStyle.Render("📚 1 context file loaded")
	}
	return statusStyle.Render(fmt.Sprintf("📚 %d context files loaded", m.ctxCount))
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
	tokens := commands.SplitArgs(text)
	if len(tokens) == 0 {
		return nil
	}
	cmd := tokens[0]
	args := ""
	if len(tokens) > 1 {
		args = strings.Join(tokens[1:], " ")
	}

	switch cmd {
	case "/help":
		helpMsg := store.Message{Role: "system", Content: helpText(m.registry), Mode: m.mode.Name()}
		m.messages = append(m.messages, helpMsg)
		_ = m.conv.Append(helpMsg)
		m.statusMsg = "loaded help"
	case "/clear":
		m.messages = nil
		m.conv.Clear()
		m.statusMsg = "conversation cleared"
	case "/mode":
		if args != "" {
			newMode, err := mode.Get(args)
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
	case "/commands":
		return m.handleCommandsBuiltin(args)
	}

	// Custom command via registry?
	if m.registry != nil {
		if c, ok := m.registry.Get(cmd); ok {
			expanded := commands.Expand(c.Prompt, args)
			m.input.SetValue(expanded)
			m.statusMsg = fmt.Sprintf("expanded /%s", c.Name)
			m.refreshViewport()
			return m.handleSubmit()
		}
	}

	if commands.IsBuiltin(cmd) {
		m.refreshViewport()
		return nil
	}

	m.statusMsg = errorStyle.Render(fmt.Sprintf("unknown command: %s (try /help)", cmd))
	m.refreshViewport()
	return nil
}

// handleCommandsBuiltin implements /commands list|reload.
func (m *Model) handleCommandsBuiltin(args string) tea.Cmd {
	sub := strings.TrimSpace(args)
	switch sub {
	case "reload":
		if m.registry == nil {
			m.statusMsg = errorStyle.Render("no registry attached")
			return nil
		}
		_ = m.registry.LoadFromDirs(
			commands.DefaultGlobalDir(),
			commands.DefaultProjectDir(cwd()),
		)
		m.statusMsg = fmt.Sprintf("reloaded: %d commands", m.registry.Count())
	case "list", "":
		if m.registry == nil {
			m.statusMsg = errorStyle.Render("no registry attached")
			return nil
		}
		body := m.registry.String()
		msg := store.Message{Role: "system", Content: "commands:\n" + body, Mode: m.mode.Name()}
		m.messages = append(m.messages, msg)
		_ = m.conv.Append(msg)
		m.statusMsg = fmt.Sprintf("%d commands", m.registry.Count())
	default:
		m.statusMsg = errorStyle.Render(fmt.Sprintf("unknown /commands subcommand: %s", sub))
	}
	m.refreshViewport()
	return nil
}

// openPicker shows the slash picker modal with the current input as the
// initial query.  No-op when no registry is attached or the input is empty.
func (m *Model) openPicker() {
	if m.registry == nil {
		m.statusMsg = "no command registry attached"
		return
	}
	p := NewPicker()
	p.SetRegistry(m.registry)
	q := strings.TrimPrefix(strings.TrimSpace(m.input.Value()), "/")
	p.Query = q
	p.SetSize(m.width, m.height)
	m.picker = p
	m.statusMsg = "pick a slash command…"
}

// cwd returns the current working directory with a sensible fallback so
// the picker can run from anywhere (including tests).
func cwd() string {
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return ""
}

func helpText(reg *commands.Registry) string {
	base := `slash commands:
  /help              show this help
  /clear             clear conversation history
  /mode [name]       switch mode (dev|reason|create|audit|universal)
  /commands list     list custom slash commands
  /commands reload   reload custom commands from disk
  /quit, /exit       exit jito

key bindings:
  Ctrl+C, Esc        quit
  Enter              send message
  Tab                open slash picker`
	if reg != nil && reg.Count() > 0 {
		base += "\ncustom commands:\n" + reg.String()
	}
	return base
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