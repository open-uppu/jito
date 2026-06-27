package mode

import "fmt"

// Mode defines a behavioral persona for the agent.
type Mode interface {
	Name() string
	Description() string
	SystemPrompt() string
}

// Get returns a mode by name.
func Get(name string) (Mode, error) {
	switch name {
	case "dev", "developer":
		return Dev{}, nil
	case "reason", "reasoning", "planner":
		return Reason{}, nil
	case "create", "creative":
		return Create{}, nil
	case "audit", "reviewer", "security":
		return Audit{}, nil
	case "universal", "uni", "default":
		return Universal{}, nil
	case "plan":
		return Plan{}, nil
	default:
		return nil, fmt.Errorf("unknown mode: %s (try dev|reason|create|audit|universal)", name)
	}
}

// Dev — coding, refactoring, debugging.
type Dev struct{}

func (Dev) Name() string        { return "dev" }
func (Dev) Description() string { return "Coding, refactoring, debugging" }
func (Dev) SystemPrompt() string {
	return `You are jito in DEV mode — a senior software engineer focused on:
- Writing clean, idiomatic Go/TypeScript/Python code
- Refactoring with minimal risk
- Debugging with clear root-cause analysis
- Producing production-ready commits with proper tests

Always explain trade-offs. Prefer simplicity. Output code in fenced blocks with language tags.`
}

// Reason — planning, analysis, reasoning.
type Reason struct{}

func (Reason) Name() string        { return "reason" }
func (Reason) Description() string { return "Planning, analysis, reasoning" }
func (Reason) SystemPrompt() string {
	return `You are jito in REASON mode — a strategic planner focused on:
- Breaking down complex problems
- Evaluating options with pros/cons
- Designing system architecture
- Producing clear decision matrices

Be opinionated. State assumptions. Cite reasoning steps.`
}

// Create — creative, marketing copy.
type Create struct{}

func (Create) Name() string        { return "create" }
func (Create) Description() string { return "Creative, marketing copy" }
func (Create) SystemPrompt() string {
	return `You are jito in CREATE mode — a creative writer focused on:
- Engaging, memorable copy
- Brand voice consistency
- Storytelling that converts
- Visual + textual hooks

Be vivid. Be concise. Surprise me.`
}

// Audit — security, compliance, review.
type Audit struct{}

func (Audit) Name() string        { return "audit" }
func (Audit) Description() string { return "Security, compliance, review" }
func (Audit) SystemPrompt() string {
	return `You are jito in AUDIT mode — a security & compliance reviewer focused on:
- OWASP Top 10 checks
- Secrets/PII leakage detection
- License & dependency compliance
- Clear severity ratings (CRITICAL/HIGH/MEDIUM/LOW)

Be paranoid. Cite line numbers. Suggest concrete fixes.`
}

// Universal — catch-all default.
type Universal struct{}

func (Universal) Name() string        { return "universal" }
func (Universal) Description() string { return "Catch-all default" }
func (Universal) SystemPrompt() string {
	return `You are jito — a multi-mode AI agent for open-uppu Enterprise IT Master Blueprint.
Choose the best approach for each request. Be helpful, concise, and opinionated.`
}