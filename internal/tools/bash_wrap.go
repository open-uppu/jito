package tools

import (
	"context"
	"fmt"

	"github.com/uppu/jito/internal/permissions"
)

// BashWrapper wraps the built-in BashTool so every execution goes through
// the permissions policy.  When Policy is nil the wrapper is a transparent
// pass-through (preserves the old behaviour for callers that have not yet
// opted in).
//
// Permission flow:
//
//  1. Approver.Decide(mode, command) returns DecisionAllow / DecisionPrompt
//     / DecisionDeny.
//  2. DecisionAllow      → execute the underlying BashTool.
//  3. DecisionDeny       → refuse; surface a "BLOCKED" message.
//  4. DecisionPrompt     → the caller is expected to have already shown
//     the approval modal and invoked Approver.Respond; if Respond returned
//     true, execute; otherwise refuse.
//
// The wrapper deliberately does not construct an approval modal itself:
// that responsibility lives in the TUI (internal/tui/approval_modal.go).
// Callers that have no TUI (e.g. the `jito run` subcommand) must wire
// their own approval flow.
type BashWrapper struct {
	Inner   BashTool
	Policy  *permissions.Policy
	Approver *permissions.Approver
}

// Name returns the wrapped tool name so the wrapper is a drop-in
// replacement for BashTool inside a tools.Registry.
func (w *BashWrapper) Name() string { return "bash" }

// Description mirrors BashTool.Description so the help text is unchanged.
func (w *BashWrapper) Description() string {
	return BashTool{}.Description()
}

// Execute runs the command after consulting the policy.  See the
// BashWrapper doc comment for the decision flow.
func (w *BashWrapper) Execute(ctx context.Context, input string) (string, error) {
	if w.Policy == nil || w.Approver == nil {
		return w.Inner.Execute(ctx, input)
	}
	mode := w.modeFor(ctx)
	decision, req := w.Approver.Decide(mode, input)
	switch decision {
	case permissions.DecisionAllow:
		return w.Inner.Execute(ctx, input)
	case permissions.DecisionDeny:
		return "", fmt.Errorf("BLOCKED: %s (mode=%s)", input, mode)
	case permissions.DecisionPrompt:
		// The TUI / CLI must have already surfaced an approval modal
		// and called Approver.Respond before reaching this point.  If
		// the session cache still says "prompt", we conservatively
		// block.
		return "", fmt.Errorf("BLOCKED (approval required): %s (mode=%s)", input, mode)
	}
	_ = req
	return "", fmt.Errorf("BLOCKED (unknown decision): %s", input)
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