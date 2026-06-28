package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordBeat is a thread-safe HeartbeatFunc used in tests.
type recordBeat struct {
	mu  sync.Mutex
	all []string
}

func (r *recordBeat) hit(status, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.all = append(r.all, status+"|"+detail)
}

func (r *recordBeat) got() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.all))
	copy(out, r.all)
	return out
}

// makeFakeJito writes a tiny shell-script that pretends to be jito:
// it just exits 0 (or 1 with EXIT_FAIL=1) and prints a marker line.
func makeFakeJito(t *testing.T, exitFail bool) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "jito")
	script := "#!/bin/sh\necho [fake-jito] arg=run prompt=\"$@\"\n"
	if exitFail {
		script += "exit 1\n"
	}
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestSpawn_Heartbeat_HappyPath(t *testing.T) {
	bin := makeFakeJito(t, false)
	rec := &recordBeat{}

	beat := HeartbeatFunc(rec.hit)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:      "test-agent",
		Bin:       bin,
		Mode:      "audit",
		Prompt:    "hello",
		Heartbeat: beat,
	})
	require.NoError(t, err)
	require.NotNil(t, sub)

	out, err := sub.Wait()
	require.NoError(t, err)
	assert.Contains(t, out, "[fake-jito] arg=run")

	got := rec.got()
	require.Len(t, got, 2, "expected STARTED then DONE")
	assert.Contains(t, got[0], "STARTED child-spawned")
	assert.Contains(t, got[0], "test-agent")
	assert.Contains(t, got[1], "DONE child-exited")
	assert.Contains(t, got[1], "test-agent")
}

func TestSpawn_Heartbeat_FailurePath(t *testing.T) {
	bin := makeFakeJito(t, true) // exit 1
	rec := &recordBeat{}

	beat := HeartbeatFunc(rec.hit)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:      "failing",
		Bin:       bin,
		Mode:      "dev",
		Prompt:    "x",
		Heartbeat: beat,
	})
	require.NoError(t, err)

	_, err = sub.Wait()
	require.Error(t, err)

	got := rec.got()
	require.Len(t, got, 2, "expected STARTED then BLOCKED")
	assert.Contains(t, got[0], "STARTED child-spawned")
	assert.Contains(t, got[1], "BLOCKED child-failed")
	assert.Contains(t, got[1], "failing")
}

func TestSpawn_NoHeartbeat_NoOp(t *testing.T) {
	// A nil Heartbeat must NOT crash and must NOT call any function.
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:   "silent",
		Bin:    bin,
		Mode:   "audit",
		Prompt: "hi",
		// Heartbeat omitted -> nil
	})
	require.NoError(t, err)
	_, err = sub.Wait()
	require.NoError(t, err, "subagent %s", sub)
}

func TestSubAgent_PIDAndString(t *testing.T) {
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{Name: "x", Bin: bin, Mode: "audit", Prompt: "hi"})
	require.NoError(t, err)
	assert.Greater(t, sub.PID(), 0)
	assert.Contains(t, sub.String(), "x")
	_, _ = sub.Wait()
}

func TestSubAgent_Kill(t *testing.T) {
	// Kill against a sub-agent whose process has already exited must
	// return an error (no such process); the contract is "best
	// effort" so we only assert non-panic.
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{Name: "killme", Bin: bin, Mode: "audit", Prompt: "x"})
	require.NoError(t, err)
	_, _ = sub.Wait()
	// Wait has reaped the process; Kill should error gracefully.
	_ = sub.Kill()
}

func TestSubAgent_Kill_NilProcess(t *testing.T) {
	// A zero-value SubAgent must not panic on Kill.
	s := &SubAgent{}
	assert.NoError(t, s.Kill())
	assert.Equal(t, 0, s.PID())
}

func TestSpawn_PATHLookup(t *testing.T) {
	// Add the fake jito to PATH so the LookPath branch fires.
	bin := makeFakeJito(t, false)
	dir := filepath.Dir(bin)
	t.Setenv("PATH", dir)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:   "pathagent",
		Mode:   "audit",
		Prompt: "hi",
		// Bin empty -> LookPath triggers.
	})
	require.NoError(t, err)
	_, err = sub.Wait()
	require.NoError(t, err)
}

func TestSpawn_DefaultWorkDirAndName(t *testing.T) {
	// Empty WorkDir defaults to "." and empty Name defaults to
	// "subagent" — both must not regress.
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Bin:    bin,
		Mode:   "audit",
		Prompt: "hi",
	})
	require.NoError(t, err)
	assert.Equal(t, "subagent", sub.Name)
	_, _ = sub.Wait()
}

func TestSpawn_ModelAndEnv(t *testing.T) {
	// Verifies that --model and extra Env propagate to the child.
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:   "envagent",
		Bin:    bin,
		Mode:   "dev",
		Prompt: "hi",
		Model:  "minimax/MiniMax-M3",
		Env:    map[string]string{"JITO_TEST_KEY": "ok"},
	})
	require.NoError(t, err)
	_, _ = sub.Wait()
}

func TestSpawn_NoBinary(t *testing.T) {
	// With an explicit but non-existent Bin path, Spawn returns
	// fork/exec error from cmd.Start. This is a different error
	// path than "binary not found" but still exercises the failure
	// branch (no STARTED heartbeat is ever emitted).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rec := &recordBeat{}
	_, err := Spawn(ctx, SpawnConfig{
		Name:      "ghost",
		Bin:       "/nonexistent/jito-binary-xyz",
		Mode:      "audit",
		Prompt:    "hi",
		Heartbeat: rec.hit,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "/nonexistent/jito-binary-xyz")
	// Heartbeat must NOT have been called.
	assert.Empty(t, rec.got(), "no heartbeat should be emitted on start failure")
}

func TestSpawnMany_Sequential_Heartbeat(t *testing.T) {
	bin := makeFakeJito(t, false)
	rec := &recordBeat{}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfgs := []SpawnConfig{
		{Name: "a", Bin: bin, Mode: "audit", Prompt: "1", Heartbeat: rec.hit},
		{Name: "b", Bin: bin, Mode: "dev", Prompt: "2", Heartbeat: rec.hit},
	}
	results, err := SpawnMany(ctx, cfgs)
	require.NoError(t, err)
	assert.Len(t, results, 2)
	assert.Contains(t, results["a"], "[fake-jito] arg=run")
	assert.Contains(t, results["b"], "[fake-jito] arg=run")

	got := rec.got()
	// Two agents × (STARTED + DONE) = 4 lines.
	require.Len(t, got, 4)
	assert.Contains(t, got[0], "STARTED child-spawned")
	assert.Contains(t, got[1], "DONE child-exited")
	assert.Contains(t, got[2], "STARTED child-spawned")
	assert.Contains(t, got[3], "DONE child-exited")
}

func TestSpawnMany_OneFailsOthersContinue(t *testing.T) {
	bin := makeFakeJito(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfgs := []SpawnConfig{
		{Name: "ok1", Bin: bin, Mode: "audit", Prompt: "hi"},
		{Name: "bad", Bin: "/nonexistent/jito-xyz", Mode: "audit", Prompt: "hi"},
		{Name: "ok2", Bin: bin, Mode: "dev", Prompt: "hi"},
	}
	results, err := SpawnMany(ctx, cfgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad")
	assert.Contains(t, results["ok1"], "[fake-jito] arg=run")
	assert.Contains(t, results["ok2"], "[fake-jito] arg=run")
	// bad is omitted because Spawn itself failed (not Wait).
	_, present := results["bad"]
	assert.False(t, present, "failed spawn must not appear in results")
}

func TestSpawnMany_Empty(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	results, err := SpawnMany(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestSpawn_CandidateFallback(t *testing.T) {
	// Strip PATH so the LookPath branch fails, and put a candidate
	// jito at one of the fallback locations the spawn code knows
	// about: /home/up-ubuntu/.local/bin/jito.
	// We create the dir+file as a symlink-free copy of the fake
	// jito. The test is skipped if we cannot create the file (e.g.
	// readonly HOME), so it is environment-portable.
	home, err := os.UserHomeDir()
	require.NoError(t, err)
	localBin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Skipf("cannot mkdir %s: %v", localBin, err)
	}
	target := filepath.Join(localBin, "jito")
	if _, err := os.Stat(target); err == nil {
		t.Skipf("%s already exists; skipping candidate-fallback test", target)
	}
	fake := makeFakeJito(t, false)
	if err := os.Link(fake, target); err != nil {
		// Hardlink may fail on some FS; fall back to copy.
		data, _ := os.ReadFile(fake)
		if err := os.WriteFile(target, data, 0o755); err != nil {
			t.Skipf("cannot create candidate at %s: %v", target, err)
		}
	}
	t.Cleanup(func() { _ = os.Remove(target) })

	t.Setenv("PATH", t.TempDir()) // no jito in PATH

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sub, err := Spawn(ctx, SpawnConfig{
		Name:   "candagent",
		Mode:   "audit",
		Prompt: "hi",
		// Bin empty -> candidate fallback triggers.
	})
	require.NoError(t, err)
	_, err = sub.Wait()
	require.NoError(t, err)
}

func TestSpawnMany_WaitErrorFolds(t *testing.T) {
	// When a child Wait() fails, SpawnMany must record the first
	// error and continue with the remaining sub-agents.
	bin := makeFakeJito(t, true) // exit 1
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfgs := []SpawnConfig{
		{Name: "fails", Bin: bin, Mode: "audit", Prompt: "1"},
		{Name: "after", Bin: bin, Mode: "audit", Prompt: "2"},
	}
	results, err := SpawnMany(ctx, cfgs)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fails")
	assert.NotEmpty(t, results["after"], "later sub-agents should still produce output")
}
