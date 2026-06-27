package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// SpawnConfig is the configuration for spawning a sub-agent.
type SpawnConfig struct {
	Name    string            // display name (e.g. "open-uppu-fe")
	WorkDir string            // working directory
	Mode    string            // jito mode (dev/reason/etc)
	Prompt  string            // initial prompt
	Model   string            // model override
	Bin     string            // explicit binary path (optional)
	Env     map[string]string // extra env vars
}

// SubAgent is a running sub-agent process.
type SubAgent struct {
	Name    string
	cmd     *exec.Cmd
	stdout  *bytes.Buffer
	stderr  *bytes.Buffer
	workDir string
}

// Spawn starts a new jito sub-agent in a separate process.
// The sub-agent runs `jito run --mode=... "prompt"` with given config.
func Spawn(ctx context.Context, cfg SpawnConfig) (*SubAgent, error) {
	if cfg.Name == "" {
		cfg.Name = "subagent"
	}
	if cfg.WorkDir == "" {
		cfg.WorkDir = "."
	}

	// Find jito binary: explicit Bin > PATH > local fallback
	binPath := cfg.Bin
	if binPath == "" {
		if path, err := exec.LookPath("jito"); err == nil {
			binPath = path
		} else {
			// Fallback: known local dev path
			candidates := []string{
				"/home/up-ubuntu/wokrspace/open-uppu/jito/bin/jito",
				"/usr/local/bin/jito",
				"/home/up-ubuntu/.local/bin/jito",
			}
			for _, c := range candidates {
				if _, err := os.Stat(c); err == nil {
					binPath = c
					break
				}
			}
		}
	}
	if binPath == "" {
		return nil, fmt.Errorf("spawn %s: jito binary not found in PATH or known locations", cfg.Name)
	}

	args := []string{"run", "--mode=" + cfg.Mode, "--no-tui"}
	if cfg.Model != "" {
		args = append(args, "--model="+cfg.Model)
	}
	args = append(args, cfg.Prompt)

	cmd := exec.CommandContext(ctx, binPath, args...)
	cmd.Dir = cfg.WorkDir

	// Build env
	env := append([]string{}, cmd.Environ()...)
	for k, v := range cfg.Env {
		env = append(env, k+"="+v)
	}
	cmd.Env = env

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w", cfg.Name, err)
	}

	return &SubAgent{
		Name:    cfg.Name,
		cmd:     cmd,
		stdout:  stdout,
		stderr:  stderr,
		workDir: cfg.WorkDir,
	}, nil
}

// Wait waits for the sub-agent to complete and returns its output.
func (s *SubAgent) Wait() (string, error) {
	err := s.cmd.Wait()
	out := s.stdout.String()
	if err != nil {
		out += "\n[stderr]\n" + s.stderr.String()
		return out, fmt.Errorf("subagent %s: %w", s.Name, err)
	}
	return strings.TrimSpace(out), nil
}

// Kill terminates the sub-agent.
func (s *SubAgent) Kill() error {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Kill()
	}
	return nil
}

// PID returns the process ID.
func (s *SubAgent) PID() int {
	if s.cmd != nil && s.cmd.Process != nil {
		return s.cmd.Process.Pid
	}
	return 0
}

// String returns a human-readable description.
func (s *SubAgent) String() string {
	return fmt.Sprintf("subagent(name=%s, pid=%d, dir=%s)", s.Name, s.PID(), s.workDir)
}

// SpawnMany runs multiple sub-agents sequentially and collects results.
// Returns map[name]output and any errors.
func SpawnMany(ctx context.Context, cfgs []SpawnConfig) (map[string]string, error) {
	results := make(map[string]string, len(cfgs))
	var firstErr error
	for _, cfg := range cfgs {
		s, err := Spawn(ctx, cfg)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		out, err := s.Wait()
		if err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: %w", cfg.Name, err)
		}
		results[cfg.Name] = out
	}
	return results, firstErr
}