package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
	"github.com/uppu/jito/internal/commands"
	jitocontext "github.com/uppu/jito/internal/context"
	"github.com/uppu/jito/internal/mode"
	"github.com/uppu/jito/internal/provider"
	"github.com/uppu/jito/internal/session"
	"github.com/uppu/jito/internal/store"
	"github.com/uppu/jito/internal/tui"
)

func newChatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Launch interactive TUI chat (Bubble Tea)",
		RunE: func(cmd *cobra.Command, args []string) error {
			modeName, _ := cmd.Flags().GetString("mode")
			modelOverride, _ := cmd.Flags().GetString("model")
			storePath, _ := cmd.Flags().GetString("store")

			m, err := mode.Get(modeName)
			if err != nil {
				return err
			}

			p, err := provider.NewFromConfig(modelOverride)
			if err != nil {
				return err
			}

			cwd, _ := os.Getwd()

			// Load JITO.md context on startup so the TUI footer can
			// show the file count. Expose via env so the TUI model
			// (which is constructed inside tui.Run) can pick it up
			// without forcing a circular import on internal/cli.
			// Done BEFORE store.Open so a store-open failure does not
			// suppress the user-facing "context loaded" log line.
			if loader, lerr := jitocontext.NewLoader(cwd); lerr == nil {
				if _, loadErr := loader.Load(); loadErr == nil {
					fmt.Printf("[jito] context: %d files loaded\n", loader.Count())
					_ = os.Setenv("JITO_CONTEXT_FILES", fmt.Sprintf("%d", loader.Count()))
				}
			}

			// Load custom slash commands (LOOP #2).  Errors are
			// non-fatal: the chat must still launch even if the user's
			// TOML has a typo.
			reg := commands.NewRegistry()
			if errs := reg.LoadFromDirs(
				commands.DefaultGlobalDir(),
				commands.DefaultProjectDir(cwd),
			); len(errs) > 0 {
				fmt.Fprintf(os.Stderr, "[jito] commands: %d parse error(s)\n", len(errs))
				for _, e := range errs {
					fmt.Fprintf(os.Stderr, "  - %v\n", e)
				}
			}
			if reg.Count() > 0 {
				fmt.Printf("[jito] commands: %d loaded\n", reg.Count())
			}

			conv, err := store.Open(storePath)
			if err != nil {
				return err
			}
			defer conv.Close()

			// Auto-checkpoint wiring (LOOP #3).  A fresh session id is
			// generated here and made visible to the TUI via the
			// JITO_SESSION_ID env var.  After the TUI exits we mirror
			// every conversation message into the sessions /
			// session_messages tables so `jito resume` and
			// `jito sessions list|show` can find the chat afterwards.
			//
			// The checkpoint itself is performed by
			// autoCheckpointSession() below; we call it whether the
			// TUI exits cleanly or returns an error so a crashed
			// session still leaves a recoverable snapshot behind.
			sessionID := uuid.NewString()
			startedAt := time.Now().UTC()
			_ = os.Setenv("JITO_SESSION_ID", sessionID)
			_ = os.Setenv("JITO_SESSION_STARTED", startedAt.Format(time.RFC3339))

			tuiErr := tui.RunWith(p, m, conv, reg)
			ckErr := autoCheckpointSession(autoCheckpointOptions{
				SessionID: sessionID,
				StartedAt: startedAt,
				Mode:      modeName,
				Model:     modelOverride,
				StorePath: storePath,
			})
			if ckErr != nil && os.Getenv("JITO_DEBUG_CHECKPOINT") != "" {
				fmt.Fprintf(os.Stderr, "[jito] checkpoint: %v\n", ckErr)
			}
			return tuiErr
		},
	}
}

// autoCheckpointOptions captures everything autoCheckpointSession
// needs to write a checkpoint, so the function is unit-testable in
// isolation from cobra.
type autoCheckpointOptions struct {
	SessionID string
	StartedAt time.Time
	Mode      string
	Model     string
	StorePath string
}

// autoCheckpointSession mirrors the conversation the user just had
// into the LOOP #3 sessions / session_messages tables so it shows up
// in `jito sessions list` and `jito resume <id>`.  The function is
// idempotent — re-running it for the same SessionID overwrites the
// snapshot.
func autoCheckpointSession(opts autoCheckpointOptions) error {
	if opts.SessionID == "" {
		return fmt.Errorf("session_id required")
	}
	st, err := store.OpenStore(opts.StorePath)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)

	// Pull the LOOP #1 conversation rows (default conv) so the
	// checkpoint reflects what the TUI saw.  The default conv holds
	// every chat message; we re-pivot into the session_messages
	// table for the resume / sessions show commands.
	conv, err := st.DefaultConversation()
	if err != nil {
		return fmt.Errorf("default conv: %w", err)
	}
	msgs := conv.Messages()

	snap := session.Snapshot{
		ID:        opts.SessionID,
		Mode:      opts.Mode,
		Model:     opts.Model,
		Title:     deriveTitle(msgs),
		CreatedAt: opts.StartedAt,
		UpdatedAt: time.Now().UTC(),
		Messages:  make([]session.SnapshotMessage, 0, len(msgs)),
	}
	for _, m := range msgs {
		snap.Messages = append(snap.Messages, session.SnapshotMessage{
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: m.CreatedAt,
		})
	}
	if err := cp.Save(snap); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

// deriveTitle picks the first user message (truncated) as the
// session's title so the `jito sessions list` output has something
// human-readable to show.
func deriveTitle(msgs []store.Message) string {
	for _, m := range msgs {
		if m.Role != "user" {
			continue
		}
		s := m.Content
		if len(s) > 60 {
			s = s[:57] + "..."
		}
		return s
	}
	return ""
}