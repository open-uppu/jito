package tools

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// DiffTool shows git diff between workdir and HEAD.
type DiffTool struct{ workDir string }

// NewDiffTool creates a diff tool scoped to workDir.
func NewDiffTool(workDir string) DiffTool {
	return DiffTool{workDir: workDir}
}

func (DiffTool) Name() string { return "diff" }
func (DiffTool) Description() string {
	return "Show git diff. Input: optional ref/path (default: HEAD)"
}

// Execute runs git diff in the workDir.
func (t DiffTool) Execute(ctx context.Context, input string) (string, error) {
	args := []string{"diff"}
	if input = strings.TrimSpace(input); input != "" {
		args = append(args, input)
	} else {
		args = append(args, "HEAD")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		// git diff returns exit code 1 if no diff — that's fine
		if strings.Contains(string(out), "No newline at end of file") || len(out) == 0 {
			return "(no diff)", nil
		}
		return "", fmt.Errorf("git diff: %w (%s)", err, string(out))
	}
	return string(out), nil
}

// PatchTool applies a unified diff to the worktree.
type PatchTool struct{ workDir string }

// NewPatchTool creates a patch tool scoped to workDir.
func NewPatchTool(workDir string) PatchTool {
	return PatchTool{workDir: workDir}
}

func (PatchTool) Name() string { return "patch" }
func (PatchTool) Description() string {
	return "Apply a unified diff. Input: file path containing the diff"
}

// Execute applies the patch file.
func (t PatchTool) Execute(ctx context.Context, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("usage: patch <path-to-diff-file>")
	}
	if !filepath.IsAbs(input) {
		input = filepath.Join(t.workDir, input)
	}
	cmd := exec.CommandContext(ctx, "git", "apply", "--check", input)
	cmd.Dir = t.workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("patch check failed: %w (%s)", err, string(out))
	}
	cmd = exec.CommandContext(ctx, "git", "apply", input)
	cmd.Dir = t.workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("patch apply failed: %w (%s)", err, string(out))
	}
	return "patch applied", nil
}

// LogTool shows git log.
type LogTool struct{ workDir string }

// NewLogTool creates a git log tool.
func NewLogTool(workDir string) LogTool {
	return LogTool{workDir: workDir}
}

func (LogTool) Name() string { return "log" }
func (LogTool) Description() string {
	return "Show recent git log. Input: optional -n <count>"
}

// Execute runs git log.
func (t LogTool) Execute(ctx context.Context, input string) (string, error) {
	args := []string{"log", "--oneline"}
	if input = strings.TrimSpace(input); input != "" {
		args = append(args, input) // e.g. "-n", "10"
	} else {
		args = append(args, "-n", "10")
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = t.workDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git log: %w (%s)", err, string(out))
	}
	return string(out), nil
}