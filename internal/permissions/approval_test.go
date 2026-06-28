package permissions

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApprover_Decide_Allowed(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	d, req := a.Decide(ModeDev, "git status")
	assert.Equal(t, DecisionAllow, d)
	assert.Nil(t, req)
}

func TestApprover_Decide_Prompt(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	d, req := a.Decide(ModeDev, "rm -rf /")
	assert.Equal(t, DecisionPrompt, d)
	require.NotNil(t, req)
	assert.Equal(t, ModeDev, req.Mode)
	assert.Equal(t, "rm -rf /", req.Command)
}

func TestApprover_Decide_Deny(t *testing.T) {
	p := NewPolicy()
	require.NoError(t, p.LoadOverrideYAML([]byte("deny:\n  dev: [rm]\n")))
	a := NewApprover(p, nil)
	d, req := a.Decide(ModeDev, "rm -rf /")
	assert.Equal(t, DecisionDeny, d)
	require.NotNil(t, req)
}

func TestApprover_Respond_AllowSession_Caches(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	a.SetSessionMode(ModeDev)
	req := &Request{Mode: ModeDev, Command: "echo hi"}

	ok := a.Respond(req, VerdictAllowSession, "trusted")
	assert.True(t, ok)
	assert.Contains(t, a.Approved(), "echo hi")

	// Second decide should now allow without re-prompting.
	d, r := a.Decide(ModeDev, "echo hi")
	assert.Equal(t, DecisionAllow, d)
	assert.Nil(t, r)
}

func TestApprover_Respond_AllowOnce_DoesNotCache(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	a.SetSessionMode(ModeDev)
	req := &Request{Mode: ModeDev, Command: "echo hi"}
	assert.True(t, a.Respond(req, VerdictAllowOnce, ""))
	assert.Empty(t, a.Approved())

	d, _ := a.Decide(ModeDev, "echo hi")
	assert.Equal(t, DecisionPrompt, d)
}

func TestApprover_Respond_Deny(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	a.SetSessionMode(ModeDev)
	req := &Request{Mode: ModeDev, Command: "rm -rf /"}
	assert.False(t, a.Respond(req, VerdictDeny, "dangerous"))
	assert.NotContains(t, a.Approved(), "rm -rf /")
}

func TestApprover_SetSessionMode_Invalidates(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	a.SetSessionMode(ModeDev)
	a.Respond(&Request{Mode: ModeDev, Command: "echo"}, VerdictAllowSession, "")
	a.SetSessionMode(ModeAudit)
	d, _ := a.Decide(ModeAudit, "echo")
	assert.Equal(t, DecisionPrompt, d, "mode switch must invalidate the cache")
}

func TestApprover_AuditLogger_Writes(t *testing.T) {
	mem := NewMemoryAudit()
	a := NewApprover(NewPolicy(), mem)
	a.SetSessionMode(ModeDev)

	a.Respond(&Request{Mode: ModeDev, Command: "echo hi"}, VerdictAllowSession, "ok")
	a.Respond(&Request{Mode: ModeDev, Command: "rm -rf /"}, VerdictDeny, "no")

	evs := mem.Events()
	require.Len(t, evs, 2)
	assert.Equal(t, VerdictAllowSession, evs[0].Verdict)
	assert.Equal(t, "ok", evs[0].Reason)
	assert.Equal(t, VerdictDeny, evs[1].Verdict)
}

func TestApprover_AuditLogger_FailingLogger(t *testing.T) {
	fl := &failingAudit{err: errors.New("disk full")}
	a := NewApprover(NewPolicy(), fl)
	a.SetSessionMode(ModeDev)
	// Respond should still return the boolean; the failure is swallowed
	// because audit logging is best-effort.
	assert.True(t, a.Respond(&Request{Mode: ModeDev, Command: "echo"}, VerdictAllowSession, ""))
}

type failingAudit struct{ err error }

func (f failingAudit) Log(Event) error { return f.err }

func TestApprover_NilPolicy_AlwaysPrompts(t *testing.T) {
	a := NewApprover(nil, nil)
	d, req := a.Decide(ModeDev, "git status")
	assert.Equal(t, DecisionPrompt, d)
	require.NotNil(t, req)
}

func TestApprover_Respond_NilRequest(t *testing.T) {
	a := NewApprover(NewPolicy(), nil)
	assert.False(t, a.Respond(nil, VerdictAllowOnce, ""))
}

func TestApprover_SetNow_OverridesClock(t *testing.T) {
	mem := NewMemoryAudit()
	a := NewApprover(NewPolicy(), mem)
	fixed := time.Date(2026, 6, 28, 14, 0, 0, 0, time.UTC)
	a.SetNow(func() time.Time { return fixed })

	a.SetSessionMode(ModeDev)
	a.Respond(&Request{Mode: ModeDev, Command: "echo"}, VerdictAllowSession, "")
	evs := mem.Events()
	require.Len(t, evs, 1)
	assert.Equal(t, fixed, evs[0].At)
}

func TestApprover_ConcurrentRespond(t *testing.T) {
	a := NewApprover(NewPolicy(), NewMemoryAudit())
	a.SetSessionMode(ModeDev)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a.Respond(&Request{Mode: ModeDev, Command: "echo"}, VerdictAllowSession, "")
		}(i)
	}
	wg.Wait()
}

func TestFormatAllowlist(t *testing.T) {
	s := FormatAllowlist(ModeDev, []string{"git", "ls"})
	assert.Contains(t, s, "dev")
	assert.Contains(t, s, "git")
	assert.Contains(t, s, "ls")

	s = FormatAllowlist(ModeReason, nil)
	assert.Contains(t, s, "none")
}

func TestMemoryAudit_DefaultTimeWhenZero(t *testing.T) {
	mem := NewMemoryAudit()
	require.NoError(t, mem.Log(Event{Command: "x"}))
	evs := mem.Events()
	require.Len(t, evs, 1)
	assert.False(t, evs[0].At.IsZero())
}

func TestVerdictString(t *testing.T) {
	assert.Equal(t, "allow-once", VerdictAllowOnce.String())
	assert.Equal(t, "allow-session", VerdictAllowSession.String())
	assert.Equal(t, "deny", VerdictDeny.String())
	assert.Equal(t, "unknown", Verdict(99).String())
}

func TestDecisionString(t *testing.T) {
	assert.Equal(t, "allow", DecisionAllow.String())
	assert.Equal(t, "prompt", DecisionPrompt.String())
	assert.Equal(t, "deny", DecisionDeny.String())
	assert.Equal(t, "unknown", Decision(99).String())
}

func TestDiscardAudit_NoPanic(t *testing.T) {
	var a AuditLogger = DiscardAudit{}
	assert.NoError(t, a.Log(Event{Command: "x"}))
}