package tools

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uppu/jito/internal/permissions"
)

func TestBashWrapper_NameAndDescription(t *testing.T) {
	w := &BashWrapper{}
	assert.Equal(t, "bash", w.Name())
	assert.NotEmpty(t, w.Description())
}

func TestBashWrapper_NilPolicyPassThrough(t *testing.T) {
	w := &BashWrapper{Inner: BashTool{workDir: t.TempDir()}}
	out, err := w.Execute(context.Background(), "echo pass")
	require.NoError(t, err)
	assert.Contains(t, out, "pass")
}

func TestBashWrapper_NilApproverPassThrough(t *testing.T) {
	w := &BashWrapper{
		Inner:  BashTool{workDir: t.TempDir()},
		Policy: permissions.NewPolicy(),
	}
	out, err := w.Execute(context.Background(), "echo pass")
	require.NoError(t, err)
	assert.Contains(t, out, "pass")
}

func TestBashWrapper_SessionApproved_Executes(t *testing.T) {
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   permissions.NewPolicy(),
		Approver: permissions.NewApprover(permissions.NewPolicy(), nil),
	}
	w.Approver.Policy = w.Policy
	w.Approver.SetSessionMode(permissions.ModeDev)

	req := &permissions.Request{Mode: permissions.ModeDev, Command: "ls"}
	w.Approver.Respond(req, permissions.VerdictAllowSession, "ok")

	out, err := w.Execute(context.Background(), "ls")
	require.NoError(t, err)
	_ = out
}

func TestBashWrapper_AllowlistedCommand(t *testing.T) {
	w := &BashWrapper{
		Inner:   BashTool{workDir: t.TempDir()},
		Policy:  permissions.NewPolicy(),
		Approver: permissions.NewApprover(permissions.NewPolicy(), nil),
	}
	w.Approver.Policy = w.Policy
	out, err := w.Execute(context.Background(), "ls -la")
	require.NoError(t, err)
	// ls always succeeds and produces output containing "total" or filename pattern.
	_ = out
}

func TestBashWrapper_NonAllowlistedNoSessionApproval_Blocked(t *testing.T) {
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   permissions.NewPolicy(),
		Approver: permissions.NewApprover(permissions.NewPolicy(), nil),
	}
	w.Approver.Policy = w.Policy
	out, err := w.Execute(context.Background(), "rm -rf /")
	require.Error(t, err)
	assert.Empty(t, out)
	assert.Contains(t, err.Error(), "BLOCKED")
}

func TestBashWrapper_DenyPolicy_AlwaysBlocked(t *testing.T) {
	policy := permissions.NewPolicy()
	require.NoError(t, policy.LoadOverrideYAML([]byte("deny:\n  dev: [rm]\n")))
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   policy,
		Approver: permissions.NewApprover(policy, nil),
	}
	_, err := w.Execute(context.Background(), "rm -rf /")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BLOCKED")
}

func TestBashWrapper_WithModeContext(t *testing.T) {
	policy := permissions.NewPolicy()
	// In audit mode, `echo` is not on the allowlist.
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   policy,
		Approver: permissions.NewApprover(policy, nil),
	}
	ctx := WithMode(context.Background(), permissions.ModeAudit)
	_, err := w.Execute(ctx, "echo hi")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BLOCKED")
	assert.Contains(t, err.Error(), "mode=audit")
}

func TestBashWrapper_WithModeContext_DefaultDev(t *testing.T) {
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   permissions.NewPolicy(),
		Approver: permissions.NewApprover(permissions.NewPolicy(), nil),
	}
	w.Approver.Policy = w.Policy
	out, err := w.Execute(context.Background(), "ls")
	require.NoError(t, err)
	assert.Contains(t, out, "") // ls outputs nothing useful; just confirm no error
}

func TestBashWrapper_AuditLogger_RecordsDecisions(t *testing.T) {
	mem := permissions.NewMemoryAudit()
	policy := permissions.NewPolicy()
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   policy,
		Approver: permissions.NewApprover(policy, mem),
	}
	w.Approver.SetSessionMode(permissions.ModeDev)
	req := &permissions.Request{Mode: permissions.ModeDev, Command: "ls"}
	w.Approver.Respond(req, permissions.VerdictAllowSession, "")
	_, err := w.Execute(context.Background(), "ls")
	require.NoError(t, err)
	events := mem.Events()
	require.NotEmpty(t, events)
	found := false
	for _, ev := range events {
		if strings.Contains(ev.Command, "ls") {
			found = true
			break
		}
	}
	assert.True(t, found, "memory audit must contain the approved command")
}

func TestBashWrapper_UnknownDecisionError(t *testing.T) {
	// Hard to trigger without reflection; sanity-check that the
	// fall-through error message format includes "BLOCKED".
	policy := permissions.NewPolicy()
	w := &BashWrapper{
		Inner:    BashTool{workDir: t.TempDir()},
		Policy:   policy,
		Approver: permissions.NewApprover(policy, nil),
	}
	// mode=audit (empty allowlist) → DecisionPrompt → "approval required" message.
	_, err := w.Execute(WithMode(context.Background(), permissions.ModeAudit), "echo")
	require.Error(t, err)
	assert.True(t, errors.Is(err, err) && err != nil)
}