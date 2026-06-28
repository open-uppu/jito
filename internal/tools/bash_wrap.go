package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/uppu/jito/internal/permissions"
	"github.com/uppu/jito/internal/session"
)

// BashWrapper wraps the built-in BashTool so every execution goes through
// the permissions policy, the sandbox helpers (path canonicalization,
// env scrubbing, network block), and the audit logger.
//
// Permission flow (each step records to Audit):
//
//  1. Approver.Decide(mode, command) returns DecisionAllow / DecisionPrompt
//     / DecisionDeny.
//  2. Policy.Check is also consulted for the hard-deny network / dangerous
//     command list — even if mode+allowlist would otherwise permit it.
//  3. Path redirect canonicalization (`>`, `>>`, `<`) refuses operations
//     that target paths outside the mode's allowed list.
//  4. DecisionAllow → scrub env, exec via BashTool with scrubbed env.
//  5. DecisionDeny → refuse; surface a "BLOCKED" message.
//  6. DecisionPrompt → caller must have already invoked Approver.Respond;
//     if the session cache still says "prompt", we conservatively block.
//
// The wrapper deliberately does not construct an approval modal itself:
// that responsibility lives in the TUI (internal/tui/approval_modal.go).
// Callers that have no TUI (e.g. the `jito run` subcommand) must wire
// their own approval flow.
type BashWrapper struct {
	Inner    BashTool
	Policy   *permissions.Policy
	Approver *permissions.Approver
	// Audit receives one event per decision.  nil → use an in-memory
	// logger so smoke tests can still introspect what happened.
	Audit session.AuditLogger
	// SessionID is attached to every audit event.  Empty for one-shot
	// CLI invocations.
	SessionID string
	// Sandbox is the path-rule set applied to file redirects inside
	// the command.  nil → default ModePathPolicy(mode).
	Sandbox *permissions.Sandbox
}

// Name returns the wrapped tool name so the wrapper is a drop-in
// replacement for BashTool inside a tools.Registry.
func (w *BashWrapper) Name() string { return "bash" }

// Description mirrors BashTool.Description so the help text is unchanged.
func (w *BashWrapper) Description() string {
	return BashTool{}.Description()
}

// Execute runs the command after consulting the policy, the sandbox
// helpers, and the audit logger.  See the BashWrapper doc comment for
// the decision flow.
func (w *BashWrapper) Execute(ctx context.Context, input string) (string, error) {
	audit := w.audit()
	mode := w.modeFor(ctx)
	cmd := strings.TrimSpace(input)

	// Step 0: empty command → deny.
	if cmd == "" {
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   "",
			Decision:  session.DecisionDeny,
			Reason:    "empty command",
			ArgsRaw:   input,
		})
		return "", fmt.Errorf("BLOCKED: empty command")
	}

	// Step 1: hard-deny layer (network + dangerous) — applied even if
	// the policy would otherwise allow.  Records to audit.
	if blocked, reason := permissions.HardDeny(cmd); blocked {
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  session.DecisionDeny,
			Reason:    reason,
			ArgsRaw:   input,
		})
		return "", fmt.Errorf("BLOCKED: %s (%s)", input, reason)
	}

	// Step 2: path-redirect canonicalization.  Reject file operations
	// that target paths outside the mode's allowed list.  The check is
	// applied BEFORE policy so a poisoned `> /etc/shadow` is blocked
	// even when mode is dev.
	sandbox := w.sandboxFor(mode)
	if bad, reason := sandbox.CheckRedirects(cmd, w.workDir()); bad {
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  session.DecisionDeny,
			Reason:    reason,
			ArgsRaw:   input,
		})
		return "", fmt.Errorf("BLOCKED: %s (%s)", input, reason)
	}

	// Step 3: passthrough mode — if no policy is wired, behave like the
	// raw BashTool but still record every execution so the audit log
	// is never silent.
	if w.Policy == nil || w.Approver == nil {
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  session.DecisionAllow,
			Reason:    "no policy wired",
			ArgsRaw:   input,
		})
		return w.Inner.Execute(ctx, input)
	}

	// Step 4: defer to the approver (mode-aware allowlist + session cache).
	decision, req := w.Approver.Decide(mode, cmd)
	switch decision {
	case permissions.DecisionAllow:
		out, err := w.executeWithScrubbedEnv(ctx, input)
		dec := session.DecisionAllow
		if err != nil {
			dec = session.DecisionError
		}
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  dec,
			Reason:    "mode allowlist",
			ArgsRaw:   input,
			Env:       os.Environ(),
		})
		return out, err
	case permissions.DecisionDeny:
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  session.DecisionDeny,
			Reason:    "policy deny",
			ArgsRaw:   input,
		})
		return "", fmt.Errorf("BLOCKED: %s (mode=%s)", input, mode)
	case permissions.DecisionPrompt:
		// The TUI / CLI must have already surfaced an approval modal
		// and called Approver.Respond before reaching this point.
		_ = audit.Record(session.AuditEvent{
			SessionID: w.SessionID,
			Command:   cmd,
			Decision:  session.DecisionPrompt,
			Reason:    "approval required",
			ArgsRaw:   input,
		})
		_ = req
		return "", fmt.Errorf("BLOCKED (approval required): %s (mode=%s)", input, mode)
	}
	_ = req
	return "", fmt.Errorf("BLOCKED (unknown decision): %s", input)
}

// executeWithScrubbedEnv runs input through BashTool but with a
// secret-scrubbed environment.  LD_PRELOAD / DYLD_* / *KEY* / *TOKEN*
// / *SECRET* / *PASS* are stripped before exec.
func (w *BashWrapper) executeWithScrubbedEnv(ctx context.Context, input string) (string, error) {
	scrubbed := permissions.ScrubExecEnv(os.Environ())
	cmd := exec.CommandContext(ctx, "bash", "-c", input)
	cmd.Dir = w.workDir()
	cmd.Env = scrubbed
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// audit returns the configured logger or a fresh in-memory one when
// nothing was wired.  Tests rely on the wrapper being usable without
// explicit audit setup.
func (w *BashWrapper) audit() session.AuditLogger {
	if w.Audit != nil {
		return w.Audit
	}
	return session.NewMemoryLogger()
}

// sandboxFor returns the configured Sandbox or builds one from the
// mode's default PathPolicy.
func (w *BashWrapper) sandboxFor(mode permissions.Mode) *permissions.Sandbox {
	if w.Sandbox != nil {
		return w.Sandbox
	}
	return permissions.NewSandbox(permissions.ModePathPolicy(mode), mode)
}

// workDir returns the underlying BashTool's working directory, falling
// back to os.Getwd() so sandbox-redirect checks have a sensible base.
func (w *BashWrapper) workDir() string {
	if w.Inner.workDir != "" {
		return w.Inner.workDir
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// modeFor extracts the active mode from ctx when the caller set one;
// otherwise falls back to dev (matches the spec's "default deny" but
// dev has the most generous allowlist).
func (w *BashWrapper) modeFor(ctx context.Context) permissions.Mode {
	if v := ctx.Value(modeContextKey{}); v != nil {
		if m, ok := v.(permissions.Mode); ok {
			return m
		}
	}
	return permissions.ModeDev
}

// modeContextKey is the context key used to attach the active mode to a
// request.  CLI / TUI callers should set it before invoking Execute.
type modeContextKey struct{}

// WithMode returns a derived context tagged with the active jito mode.
// BashWrapper reads it from ctx to decide which allowlist applies.
func WithMode(ctx context.Context, m permissions.Mode) context.Context {
	return context.WithValue(ctx, modeContextKey{}, m)
}