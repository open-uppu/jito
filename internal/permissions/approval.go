package permissions

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

// Verdict is the user's response to an approval prompt.
type Verdict int

const (
	// VerdictAllowOnce permits this single execution and adds nothing
	// to the in-memory cache.
	VerdictAllowOnce Verdict = iota
	// VerdictAllowSession permits this execution and remembers it for
	// the rest of the session (so the user is not re-prompted for the
	// same command).
	VerdictAllowSession
	// VerdictDeny blocks this execution; the caller must surface the
	// reason (if any) back to the user / LLM.
	VerdictDeny
)

func (v Verdict) String() string {
	switch v {
	case VerdictAllowOnce:
		return "allow-once"
	case VerdictAllowSession:
		return "allow-session"
	case VerdictDeny:
		return "deny"
	default:
		return "unknown"
	}
}

// Event is one row of the approval audit log.  The actual persistence is
// deferred to LOOP #3 (internal/session/audit.go); for now the
// AuditLogger interface is satisfied by an in-memory implementation.
type Event struct {
	At      time.Time
	Mode    Mode
	Command string
	Verdict Verdict
	Reason  string
}

// AuditLogger receives every approval decision.  The interface is kept
// tiny so the eventual LOOP #3 implementation can swap in a real
// append-only writer without changing the call sites.
type AuditLogger interface {
	Log(ev Event) error
}

// DiscardAudit is the no-op logger used by tests and by code paths that
// have not yet wired the real audit writer.
type DiscardAudit struct{}

// Log satisfies AuditLogger.
func (DiscardAudit) Log(Event) error { return nil }

// MemoryAudit is an in-process AuditLogger useful for tests and
// short-lived CLI invocations.
type MemoryAudit struct {
	mu     sync.Mutex
	events []Event
}

// NewMemoryAudit returns an empty MemoryAudit.
func NewMemoryAudit() *MemoryAudit { return &MemoryAudit{} }

// Log appends ev to the in-memory log.
func (m *MemoryAudit) Log(ev Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	m.events = append(m.events, ev)
	return nil
}

// Events returns a copy of the recorded events.
func (m *MemoryAudit) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// Approver answers approval requests.  It is decoupled from the policy
// so callers can inject pre-recorded verdicts in tests.
type Approver struct {
	Policy      *Policy
	Audit       AuditLogger
	nowFn       func() time.Time

	mu          sync.Mutex
	session     map[string]Verdict // command → verdict for session lifetime
	sessionMode Mode
}

// NewApprover wires an Approver to policy and audit logger.  Pass nil
// for audit to use DiscardAudit (matches the "defer to LOOP #3" stub).
func NewApprover(p *Policy, audit AuditLogger) *Approver {
	if audit == nil {
		audit = DiscardAudit{}
	}
	return &Approver{
		Policy:  p,
		Audit:   audit,
		nowFn:   time.Now,
		session: make(map[string]Verdict),
	}
}

// SetNow overrides the clock (tests).
func (a *Approver) SetNow(fn func() time.Time) { a.nowFn = fn }

// SetSessionMode caches the active mode so the session cache can be
// scoped to it (a /mode switch invalidates prior approvals).
func (a *Approver) SetSessionMode(m Mode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.sessionMode = m
	a.session = make(map[string]Verdict) // mode switch invalidates session
}

// Request represents an in-flight approval request.  The TUI fills it
// out, the Approver.Respond method applies it.
type Request struct {
	Mode    Mode
	Command string
	Reason  string
}

// Decide consults the policy and the session cache.  It returns:
//
//	(DecisionAllow, "")  — already approved or on allowlist; execute.
//	(DecisionPrompt, "") — user must be prompted; returns a Request
//	                       that the TUI can render.
//	(DecisionDeny, "…")  — hard-blocked; never executable.
func (a *Approver) Decide(mode Mode, command string) (Decision, *Request) {
	if a.Policy == nil {
		return DecisionPrompt, &Request{Mode: mode, Command: command}
	}
	d := a.Policy.Check(mode, command)
	switch d {
	case DecisionAllow:
		return DecisionAllow, nil
	case DecisionDeny:
		return DecisionDeny, &Request{Mode: mode, Command: command, Reason: "command denied by policy"}
	case DecisionPrompt:
		a.mu.Lock()
		v, ok := a.session[command]
		modeMatch := a.sessionMode == mode
		a.mu.Unlock()
		if ok && modeMatch && (v == VerdictAllowSession || v == VerdictAllowOnce) {
			return DecisionAllow, nil
		}
		return DecisionPrompt, &Request{Mode: mode, Command: command}
	}
	return DecisionPrompt, &Request{Mode: mode, Command: command}
}

// Respond applies the user's verdict to the request and writes it to the
// audit log.  The boolean return reports whether the command may now
// execute.
func (a *Approver) Respond(req *Request, verdict Verdict, reason string) bool {
	if req == nil {
		return false
	}
	if req.Reason == "" {
		req.Reason = reason
	}
	_ = a.Audit.Log(Event{
		At:      a.nowFn(),
		Mode:    req.Mode,
		Command: req.Command,
		Verdict: verdict,
		Reason:  reason,
	})
	switch verdict {
	case VerdictAllowSession:
		a.mu.Lock()
		a.session[req.Command] = VerdictAllowSession
		a.mu.Unlock()
		return true
	case VerdictAllowOnce:
		// Don't cache; prompt again next time.
		return true
	case VerdictDeny:
		return false
	}
	return false
}

// Approved returns the set of session-cached command tokens (used by the
// /commands list debug output and tests).
func (a *Approver) Approved() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]string, 0, len(a.session))
	for k := range a.session {
		out = append(out, k)
	}
	return out
}

// FormatAllowlist is a tiny formatter used by the approval modal to
// render "Allowed in <mode>: <comma list>".  An empty allowlist renders
// as "none".
func FormatAllowlist(mode Mode, list []string) string {
	if len(list) == 0 {
		return fmt.Sprintf("Allowed in %s: none (every command requires approval)", mode)
	}
	return fmt.Sprintf("Allowed in %s: %s", mode, strings.Join(list, ", "))
}