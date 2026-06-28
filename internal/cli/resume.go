package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/uppu/jito/internal/session"
	"github.com/uppu/jito/internal/store"
)

// resumeOptions captures all inputs to ExecuteResume so the function
// is testable without invoking cobra.
type resumeOptions struct {
	SessionID string
	StorePath string
	NewMode   string // optional --mode override
	AsJSON    bool   // --json: print the full snapshot as JSON
}

// ExecuteResume loads a saved session from the SQLite store and prints
// it in human-readable (default) or JSON (--json) form.  This is the
// read-only counterpart of `jito chat` and is used to inspect a
// previous session without launching the TUI.
func ExecuteResume(opts resumeOptions) (string, error) {
	st, err := store.OpenStore(opts.StorePath)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)
	target := opts.SessionID

	// Allow short-id resolution: if the user passed a prefix that
	// uniquely matches a stored session id, expand it.  This only
	// runs when the prefix is shorter than any candidate full id;
	// exact-id input is passed straight to cp.Load.
	if target != "" {
		all, lerr := cp.List()
		if lerr != nil {
			return "", fmt.Errorf("list sessions: %w", lerr)
		}
		// If the target is already an exact match, skip the prefix scan.
		exact := false
		for _, s := range all {
			if s.ID == target {
				exact = true
				break
			}
		}
		if !exact {
			var matches []string
			for _, s := range all {
				if strings.HasPrefix(s.ID, target) {
					matches = append(matches, s.ID)
				}
			}
			switch len(matches) {
			case 0:
				return "", fmt.Errorf("session %q not found", target)
			case 1:
				target = matches[0]
			default:
				return "", fmt.Errorf("session %q is ambiguous (%d matches: %s)",
					target, len(matches), strings.Join(matches, ", "))
			}
		}
	}

	if target == "" {
		// No id provided → pick the most recently updated session.
		all, lerr := cp.List()
		if lerr != nil {
			return "", fmt.Errorf("list sessions: %w", lerr)
		}
		if len(all) == 0 {
			return "", fmt.Errorf("no sessions to resume; use 'jito chat' to start one")
		}
		target = all[0].ID
	}

	snap, err := cp.Load(target)
	if err != nil {
		return "", fmt.Errorf("load session %s: %w", target, err)
	}
	if opts.NewMode != "" {
		snap.Mode = opts.NewMode
	}

	if opts.AsJSON {
		buf, mErr := json.MarshalIndent(snap, "", "  ")
		if mErr != nil {
			return "", mErr
		}
		return string(buf), nil
	}
	return renderSnapshot(snap), nil
}

// renderSnapshot formats a Snapshot for terminal display.
func renderSnapshot(s session.Snapshot) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Session:  %s\n", s.ID)
	fmt.Fprintf(&sb, "Title:    %s\n", fallback(s.Title, "(untitled)"))
	fmt.Fprintf(&sb, "Mode:     %s\n", s.Mode)
	fmt.Fprintf(&sb, "Model:    %s\n", fallback(s.Model, "(default)"))
	if s.ParentID != "" {
		fmt.Fprintf(&sb, "Parent:   %s\n", s.ParentID)
	}
	fmt.Fprintf(&sb, "Created:  %s\n", s.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Updated:  %s\n", s.UpdatedAt.Format(time.RFC3339))
	fmt.Fprintf(&sb, "Messages: %d\n", len(s.Messages))
	fmt.Fprintln(&sb, strings.Repeat("-", 60))
	for i, m := range s.Messages {
		ts := m.CreatedAt.Format(time.RFC3339)
		fmt.Fprintf(&sb, "[%02d] %s @ %s\n%s\n", i+1, m.Role, ts, indent(m.Content, "    "))
	}
	return sb.String()
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func indent(s, pad string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}

func newResumeCmd() *cobra.Command {
	var asJSON bool
	var newMode string
	cmd := &cobra.Command{
		Use:   "resume [<session-id>]",
		Short: "Resume a previously checkpointed chat session",
		Long: `Resume prints the full message history of a saved session.

If <session-id> is omitted the most recently updated session is
selected.  A short prefix that uniquely matches one session id is
also accepted.  Use --json to dump the raw snapshot for piping to
other tools.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var id string
			if len(args) > 0 {
				id = args[0]
			}
			storePath, _ := cmd.Flags().GetString("store")
			out, err := ExecuteResume(resumeOptions{
				SessionID: id,
				StorePath: storePath,
				NewMode:   newMode,
				AsJSON:    asJSON,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the full snapshot as JSON")
	cmd.Flags().StringVar(&newMode, "mode", "", "override the session's mode label for display")
	return cmd
}

// ensureUUID is a tiny helper used by chat.go's auto-checkpoint path.
// Keeping it here avoids a direct uuid import in chat.go.
func ensureUUID(id string) string {
	if id == "" {
		return uuid.NewString()
	}
	return id
}

// ResetSessionID is a no-op helper kept around for symmetry with
// ensureUUID; the chat path uses it to mark a "checkpoint boundary".
func ResetSessionID(current string) string {
	_ = current
	return ""
}

// Suppress unused-import warnings when this file is built without
// the chat.go consumers referencing it.
var _ = os.Stdout