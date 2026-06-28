package cli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/uppu/jito/internal/session"
	"github.com/uppu/jito/internal/store"
)

// sessionsListOptions captures inputs to ExecuteSessionsList for testing.
type sessionsListOptions struct {
	StorePath string
	AsJSON    bool
	Limit     int
}

// sessionsShowOptions captures inputs to ExecuteSessionsShow for testing.
type sessionsShowOptions struct {
	SessionID string
	StorePath string
	AsJSON    bool
}

// sessionsDeleteOptions captures inputs to ExecuteSessionsDelete for testing.
type sessionsDeleteOptions struct {
	SessionID string
	StorePath string
	Force     bool
}

// ExecuteSessionsList returns one line per saved session, ordered by
// most recently updated.  Output is human-readable unless AsJSON is set.
func ExecuteSessionsList(opts sessionsListOptions) (string, error) {
	st, err := store.OpenStore(opts.StorePath)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)
	snaps, err := cp.List()
	if err != nil {
		return "", fmt.Errorf("list sessions: %w", err)
	}

	// Sort by UpdatedAt DESC, deterministic tie-break by ID.
	sort.Slice(snaps, func(i, j int) bool {
		if snaps[i].UpdatedAt.Equal(snaps[j].UpdatedAt) {
			return snaps[i].ID > snaps[j].ID
		}
		return snaps[i].UpdatedAt.After(snaps[j].UpdatedAt)
	})
	if opts.Limit > 0 && opts.Limit < len(snaps) {
		snaps = snaps[:opts.Limit]
	}

	if opts.AsJSON {
		buf, jerr := json.MarshalIndent(snaps, "", "  ")
		if jerr != nil {
			return "", jerr
		}
		return string(buf), nil
	}

	if len(snaps) == 0 {
		return "No saved sessions. Run `jito chat` to create one.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%-36s  %-7s  %-19s  %-19s  %4s  %s\n",
		"ID", "MODE", "UPDATED", "CREATED", "MSGS", "TITLE")
	fmt.Fprintln(&sb, strings.Repeat("-", 120))
	for _, s := range snaps {
		id := s.ID
		if len(id) > 36 {
			id = id[:33] + "..."
		}
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		fmt.Fprintf(&sb, "%-36s  %-7s  %-19s  %-19s  %4d  %s\n",
			id,
			s.Mode,
			s.UpdatedAt.UTC().Format(time.RFC3339),
			s.CreatedAt.UTC().Format(time.RFC3339),
			len(s.Messages),
			title)
	}
	return sb.String(), nil
}

// ExecuteSessionsShow prints the full snapshot for one session (same
// as `jito resume <id>` but without the implicit "most recent" fallback).
func ExecuteSessionsShow(opts sessionsShowOptions) (string, error) {
	st, err := store.OpenStore(opts.StorePath)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)
	snap, err := cp.Load(opts.SessionID)
	if err != nil {
		return "", fmt.Errorf("load session %s: %w", opts.SessionID, err)
	}
	if opts.AsJSON {
		buf, jerr := json.MarshalIndent(snap, "", "  ")
		if jerr != nil {
			return "", jerr
		}
		return string(buf), nil
	}
	return renderSnapshot(snap), nil
}

// ExecuteSessionsDelete removes a session and its messages.  When
// Force is false, the caller is expected to have prompted the user
// already (the CLI flag is --yes).
func ExecuteSessionsDelete(opts sessionsDeleteOptions) (string, error) {
	st, err := store.OpenStore(opts.StorePath)
	if err != nil {
		return "", fmt.Errorf("open store: %w", err)
	}
	defer st.Close()

	cp := session.NewStoreCheckpointer(st)
	if !opts.Force {
		// Without --yes we refuse so the user doesn't accidentally
		// wipe a checkpoint they wanted to keep.
		return "", fmt.Errorf("refusing to delete session %q without --yes", opts.SessionID)
	}
	if err := cp.Delete(opts.SessionID); err != nil {
		return "", fmt.Errorf("delete session %s: %w", opts.SessionID, err)
	}
	return fmt.Sprintf("deleted session %s", opts.SessionID), nil
}

func newSessionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "Manage saved chat sessions",
		Long: `Manage saved chat sessions.

Subcommands:
  list              List every saved session (most recent first).
  show <id>         Print the full message history of one session.
  delete <id>       Remove a session (requires --yes).

Use --json on list/show to dump the raw snapshot for piping.`,
	}

	// list
	var listJSON bool
	var listLimit int
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List saved sessions",
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, _ := cmd.Flags().GetString("store")
			out, err := ExecuteSessionsList(sessionsListOptions{
				StorePath: storePath,
				AsJSON:    listJSON,
				Limit:     listLimit,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	listCmd.Flags().BoolVar(&listJSON, "json", false, "print as JSON")
	listCmd.Flags().IntVar(&listLimit, "limit", 0, "limit output to N most-recent sessions (0 = no limit)")

	// show
	var showJSON bool
	showCmd := &cobra.Command{
		Use:   "show <session-id>",
		Short: "Show full session history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, _ := cmd.Flags().GetString("store")
			out, err := ExecuteSessionsShow(sessionsShowOptions{
				SessionID: args[0],
				StorePath: storePath,
				AsJSON:    showJSON,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	showCmd.Flags().BoolVar(&showJSON, "json", false, "print as JSON")

	// delete
	var delYes bool
	deleteCmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session (requires --yes)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			storePath, _ := cmd.Flags().GetString("store")
			out, err := ExecuteSessionsDelete(sessionsDeleteOptions{
				SessionID: args[0],
				StorePath: storePath,
				Force:     delYes,
			})
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), out)
			return nil
		},
	}
	deleteCmd.Flags().BoolVar(&delYes, "yes", false, "skip the confirmation prompt")

	cmd.AddCommand(listCmd, showCmd, deleteCmd)
	return cmd
}