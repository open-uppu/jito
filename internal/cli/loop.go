package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/uppu/jito/internal/loop"
)

// newLoopCmd wires `jito loop` for inspecting the CEO-Profile Loop
// Engineering state from the CLI.
//
// Subcommands:
//
//	state    — print STATE.md
//	run-log  — print today's run-log (or --date=YYYY-MM-DD)
//	status   — one-line summary: loops, last entry, blockers count
//
// All commands honour --state-dir= to override the default location
// (~/.openclaw/workspace/state/loop-engineering). The default is
// identical to what internal/loop uses, so loop ops and the CLI see
// the same data.
func newLoopCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "loop",
		Short: "Inspect CEO-Profile Loop Engineering state (STATE.md + run-log)",
		Long: `loop surfaces the durable Loop Engineering state owned by
CEO-Profile. It is the human-facing mirror of the same files that
internal/loop reads/writes during a jito spawn.

Subcommands:
  state    print STATE.md verbatim
  run-log  print run-log entries (default: today; --date=YYYY-MM-DD)
  status   one-line summary: loop count, last entry, blockers`,
	}

	// Persistent flags shared across sub-commands.
	cmd.PersistentFlags().String("state-dir", "",
		"override loop-engineering state dir (default: ~/.openclaw/workspace/state/loop-engineering)")
	cmd.PersistentFlags().Bool("json", false, "emit JSON instead of human-readable text")

	cmd.AddCommand(newLoopStateCmd())
	cmd.AddCommand(newLoopRunLogCmd())
	cmd.AddCommand(newLoopStatusCmd())
	return cmd
}

// engineFromCmd resolves the Config from the persistent flags.
func engineFromCmd(cmd *cobra.Command) (*loop.Engine, error) {
	dir, _ := cmd.Flags().GetString("state-dir")
	cfg := loop.Config{StateDir: dir}
	return loop.New(cfg)
}

func newLoopStateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "state",
		Short: "Print STATE.md verbatim",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := engineFromCmd(cmd)
			if err != nil {
				return err
			}
			data, err := e.ReadState()
			if err != nil {
				return err
			}
			_, err = cmd.OutOrStdout().Write(data)
			return err
		},
	}
}

func newLoopRunLogCmd() *cobra.Command {
	var date string
	c := &cobra.Command{
		Use:   "run-log",
		Short: "Print run-log entries (default: today)",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := engineFromCmd(cmd)
			if err != nil {
				return err
			}

			var when time.Time
			if date != "" {
				when, err = time.Parse("2006-01-02", date)
				if err != nil {
					return fmt.Errorf("loop run-log: bad --date=%q: %w", date, err)
				}
			}

			entries, invalid, err := e.ReadRunLog(when)
			if err != nil {
				return err
			}

			asJSON, _ := cmd.Flags().GetBool("json")
			out := cmd.OutOrStdout()
			if asJSON {
				return writeRunLogJSON(out, entries, invalid)
			}
			return writeRunLogHuman(out, entries, invalid)
		},
	}
	c.Flags().StringVar(&date, "date", "", "date filter (YYYY-MM-DD, default: today UTC)")
	return c
}

func newLoopStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "One-line summary: loops, last entry, blockers",
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := engineFromCmd(cmd)
			if err != nil {
				return err
			}

			entries, _, err := e.ReadRunLog(time.Time{})
			if err != nil {
				// Tolerate missing run-log — still print state.
				entries = nil
			}
			stateBytes, stateErr := e.ReadState()
			loops, blockers := summariseState(string(stateBytes))

			asJSON, _ := cmd.Flags().GetBool("json")
			out := cmd.OutOrStdout()
			if asJSON {
				payload := map[string]any{
					"state_dir": e.StateDir(),
					"loops":     loops,
					"blockers":  blockers,
					"entries":   len(entries),
				}
				if len(entries) > 0 {
					last := entries[len(entries)-1]
					payload["last_entry"] = map[string]any{
						"status": last.Status,
						"task":   last.TaskID,
						"detail": last.Detail,
					}
				}
				if stateErr != nil {
					payload["state_error"] = stateErr.Error()
				}
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(payload)
			}

			fmt.Fprintf(out, "state_dir: %s\n", e.StateDir())
			fmt.Fprintf(out, "loops:     %d\n", loops)
			fmt.Fprintf(out, "blockers:  %d\n", blockers)
			fmt.Fprintf(out, "entries:   %d\n", len(entries))
			if len(entries) > 0 {
				last := entries[len(entries)-1]
				fmt.Fprintf(out, "last:      [%s] %s — %s\n",
					last.Status, last.TaskID, truncate(last.Detail, 80))
			}
			if stateErr != nil {
				fmt.Fprintf(out, "state:     <missing: %s>\n", stateErr)
			}
			return nil
		},
	}
}


// writeRunLogHuman renders entries in their canonical on-disk form.
func writeRunLogHuman(out io.Writer, entries []loop.Entry, invalid []string) error {
	for _, e := range entries {
		ts := e.Timestamp.Format("15:04:05")
		if e.Loop == "" {
			// Defensive — StrictRegex guarantees Loop is non-empty,
			// but a future migration could change that.
			fmt.Fprintf(out, "%s ? | %s | %s\n", ts, e.TaskID, e.Status)
			continue
		}
		fmt.Fprintf(out, "%s GMT+7 | %s | %s | %s | %s\n",
			ts, e.Loop, e.TaskID, e.Status, e.Detail)
	}
	if len(invalid) > 0 {
		fmt.Fprintf(out, "\n# %d invalid lines (regex mismatch):\n", len(invalid))
		for _, line := range invalid {
			fmt.Fprintf(out, "# INVALID: %s\n", line)
		}
	}
	return nil
}

func writeRunLogJSON(out io.Writer, entries []loop.Entry, invalid []string) error {
	payload := struct {
		Entries []loop.Entry `json:"entries"`
		Invalid []string     `json:"invalid,omitempty"`
	}{
		Entries: entries,
		Invalid: invalid,
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// summariseState extracts a best-effort (loops, blockers) tuple from
// the raw STATE.md body. It is deliberately tolerant: any line that
// looks like a loop row (| Loop | … or | #N |) is counted as one
// loop, and any row matching the known-blocks table format (| B-N |)
// OR containing the literal word "blocker" (case-insensitive) is
// tallied as a blocker. Returns (0, 0) on parse failure or empty
// input.
func summariseState(body string) (loops, blockers int) {
	if body == "" {
		return 0, 0
	}
	for _, line := range strings.Split(body, "\n") {
		l := strings.ToLower(line)
		if strings.Contains(l, "| loop") || strings.Contains(l, "| #") {
			loops++
		}
		if strings.Contains(l, "| b-") || strings.Contains(l, "blocker") {
			blockers++
		}
	}
	if loops > 0 {
		// Decrement the header row ("| Loop | Target | …").
		loops--
	}
	if loops < 0 {
		loops = 0
	}
	return loops, blockers
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	// Reserve one rune for the ellipsis so the result is exactly n bytes.
	return s[:n-1] + "…"
}

// (no package-level vars needed)
