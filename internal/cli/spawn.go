package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/uppu/jito/internal/agent"
	"github.com/uppu/jito/internal/loop"
)

// newSpawnCmd wires `jito spawn <agent-name> <task>` for the
// CEO-Profile Loop Engineering layer.
//
// When invoked, this command:
//
//   1. Resolves the loop-engineering Engine (state-dir overridable
//      via --state-dir).
//   2. Appends a STARTED heartbeat line.
//   3. Forks a real jito sub-agent (via internal/agent.Spawn) with
//      the loop Engine wired in as its Heartbeat callback — every
//      child spawn/done emits its own heartbeat automatically.
//   4. Waits for the child, appends DONE / BLOCKED, prints the
//      captured stdout.
//
// Examples:
//
//	jito spawn jito-test "run unit tests"
//	jito spawn jito-test --loop=5 --task=context-loader "exercise JITO.md"
//	jito spawn jito-test --dry-run --loop=4 --task=jito-rel-LOOP4 "smoke"
//
// The --dry-run flag is a CEO-friendly shortcut: it appends the
// STARTED + DONE lines to the run-log WITHOUT forking a sub-agent.
// Useful for sub-agent bookkeeping during non-Loop-Engineering work
// (e.g. PM documenting a stand-up).
func newSpawnCmd() *cobra.Command {
	var (
		loopID  string // e.g. "4" -> rendered as "LOOP#4"
		taskID  string // e.g. "jito-rel-LOOP4"
		mode    string
		model   string
		workDir string
		binary  string
		dryRun  bool
	)

	c := &cobra.Command{
		Use:   "spawn <agent-name> <task-prompt>",
		Short: "Spawn a jito sub-agent and announce it to the Loop Engineering layer",
		Long: `spawn launches a sub-agent and announces its lifecycle to the
CEO-Profile Loop Engineering state (STATE.md + run-log).

It is the entry point used by PM (main session) and jito-rel to drive
the per-loop pipeline. Pass --dry-run to skip forking and only emit
the heartbeat entries.

Required positional args:
  <agent-name>   human-readable name (e.g. "jito-test", "jito-rel")
  <task-prompt>  initial prompt for the sub-agent`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			prompt := args[1]

			if taskID == "" {
				taskID = name
			}
			loopStr := "LOOP#" + loopID
			if loopID == "" {
				loopStr = "LOOP#?"
			}

			dir, _ := cmd.Flags().GetString("state-dir")
			engine, err := loop.New(loop.Config{StateDir: dir})
			if err != nil {
				return err
			}

			// Local helper — appends one line through the engine.
			beat := func(status, detail string) {
				_ = engine.Append(loop.Entry{
					Timestamp: time.Now(),
					Loop:      loopStr,
					TaskID:    taskID,
					Status:    status,
					Detail:    detail,
				})
			}

			if mode == "" {
				mode = "universal"
			}

			if dryRun {
				beat("STARTED", fmt.Sprintf("dry-run spawn %s prompt=%q mode=%s", name, trim(prompt, 60), mode))
				beat("DONE dry-run", fmt.Sprintf("spawn %s completed (no child forked) prompt=%q", name, trim(prompt, 60)))
				fmt.Fprintln(cmd.OutOrStdout(), "(dry-run) — no sub-agent forked; heartbeat written")
				return nil
			}

			cwd := workDir
			if cwd == "" {
				cwd, _ = os.Getwd()
			}

			beat("STARTED", fmt.Sprintf("spawn %s prompt=%q mode=%s cwd=%s",
				name, trim(prompt, 60), mode, cwd))

			ctx, cancel := context.WithCancel(cmd.Context())
			defer cancel()

			sub, err := agent.Spawn(ctx, agent.SpawnConfig{
				Name:      name,
				WorkDir:   cwd,
				Mode:      mode,
				Prompt:    prompt,
				Model:     model,
				Bin:       binary,
				Heartbeat: beat,
			})
			if err != nil {
				beat("BLOCKED", fmt.Sprintf("spawn %s failed: %v", name, err))
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "🟢 spawned %s (pid=%d)\n", sub.Name, sub.PID())

			out, err := sub.Wait()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "🔴 %s exited with error: %v\n", sub.Name, err)
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "── sub-agent output ──")
			fmt.Fprintln(cmd.OutOrStdout(), out)
			fmt.Fprintln(cmd.OutOrStdout(), "── end output ──")

			// The Heartbeat callback has already emitted DONE /
			// BLOCKED via agent.Wait; nothing more to write here.
			return nil
		},
	}

	c.Flags().StringVar(&loopID, "loop", "?", "loop number (rendered as LOOP#N)")
	c.Flags().StringVar(&taskID, "task", "", "task ID for heartbeat entries (default: agent name)")
	c.Flags().StringVar(&mode, "mode", "universal", "sub-agent mode (dev|reason|create|audit|universal)")
	c.Flags().StringVar(&model, "model", "", "model override")
	c.Flags().StringVar(&workDir, "workdir", "", "working directory (default: $PWD)")
	c.Flags().StringVar(&binary, "bin", "", "jito binary path (default: PATH)")
	c.Flags().BoolVar(&dryRun, "dry-run", false, "skip forking; emit STARTED+DONE only")
	c.Flags().String("state-dir", "", "loop-engineering state dir (default: ~/.openclaw/workspace/state/loop-engineering)")
	return c
}

// trim collapses whitespace and clips a string to n runes.
func trim(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}
