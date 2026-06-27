package mode

import (
	"context"
	"fmt"
	"strings"

	"github.com/uppu/jito/internal/provider"
)

// Plan is a read-only analysis mode.
// Won't make file changes — only proposes them.
type Plan struct{}

// Name returns the mode name.
func (Plan) Name() string { return "plan" }

// Description returns a short description.
func (Plan) Description() string { return "Read-only analysis with proposed changes" }

// SystemPrompt returns the persona prompt.
func (Plan) SystemPrompt() string {
	return `You are jito in PLAN mode — a read-only analysis specialist focused on:
- Understanding codebases without making changes
- Proposing concrete refactors/edits as markdown diffs
- Listing affected files + risk assessment
- Estimating impact (lines changed, complexity)

RULES:
- NEVER write or modify files
- Output proposed changes as unified diff format
- Use 'jito diff'-style output for clarity
- Always end with "## Next Steps" section listing files to edit
- Be concrete, cite line numbers when possible`
}

// PlanOutput is a structured result from plan mode.
type PlanOutput struct {
	Summary    string
	Steps      []string
	Diff       string
	RiskLevel  string // LOW/MEDIUM/HIGH
	FilesCount int
}

// Execute runs the plan mode against a provider.
func Execute(ctx context.Context, p provider.Provider, prompt string) (*PlanOutput, error) {
	m := Plan{}
	resp, err := p.Chat(ctx, m.SystemPrompt(), prompt)
	if err != nil {
		return nil, fmt.Errorf("plan: %w", err)
	}

	out := parsePlan(resp)
	return out, nil
}

func parsePlan(resp string) *PlanOutput {
	out := &PlanOutput{
		Summary: resp,
		RiskLevel: "MEDIUM",
	}

	// Extract diff block
	if start := strings.Index(resp, "```diff"); start >= 0 {
		end := strings.Index(resp[start+7:], "```")
		if end >= 0 {
			out.Diff = strings.TrimSpace(resp[start+7 : start+7+end])
		}
	}

	// Extract steps from "## Next Steps" section
	if idx := strings.Index(resp, "## Next Steps"); idx >= 0 {
		rest := resp[idx:]
		lines := strings.Split(rest, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				out.Steps = append(out.Steps, strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "))
			}
		}
	}

	// Estimate risk from keywords
	lower := strings.ToLower(resp)
	switch {
	case strings.Contains(lower, "critical"), strings.Contains(lower, "breaking change"):
		out.RiskLevel = "HIGH"
	case strings.Contains(lower, "minor"), strings.Contains(lower, "low risk"):
		out.RiskLevel = "LOW"
	}

	// Count unique files in diff
	files := map[string]bool{}
	for _, line := range strings.Split(out.Diff, "\n") {
		if strings.HasPrefix(line, "diff --git") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				path := strings.TrimPrefix(parts[3], "b/")
				files[path] = true
			}
		}
	}
	out.FilesCount = len(files)

	return out
}