package commands

import (
	"fmt"
	"strings"
)

// Expand substitutes the {{args}} placeholder in the prompt template.
//
// Behaviour:
//   - tokens occurring outside a !{...} block are replaced with the raw
//     user-provided args (no quoting),
//   - tokens occurring inside a !{...} block are shell-escaped (POSIX
//     single-quote with embedded single-quote escaping) so the rendered
//     snippet can be embedded in another shell script safely.
//
// The function processes the template left-to-right.  !{ opens a shell
// block, } closes it.  Blocks do NOT nest.  Unmatched braces are passed
// through literally so the user sees a typo.
func Expand(template, args string) string {
	var b strings.Builder
	b.Grow(len(template) + len(args))
	i := 0
	for i < len(template) {
		// Detect a !{ ... } shell block.
		if strings.HasPrefix(template[i:], "!{") {
			end := indexShellBlockEnd(template[i+2:])
			if end < 0 {
				// unmatched !{ — emit the rest verbatim
				b.WriteString(template[i:])
				return b.String()
			}
			// template[i+2 : i+2+end] is the block body (without braces)
			body := template[i+2 : i+2+end]
			expanded := expandWithEscape(body, args)
			b.WriteByte('!')
			b.WriteByte('{')
			b.WriteString(expanded)
			b.WriteByte('}')
			i += 2 + end + 1 // consume "!{" + body + "}"
			continue
		}
		// Detect {{args}}.
		if strings.HasPrefix(template[i:], "{{args}}") {
			b.WriteString(args)
			i += len("{{args}}")
			continue
		}
		b.WriteByte(template[i])
		i++
	}
	return b.String()
}

// indexShellBlockEnd returns the index of the closing "}" of a !{...}
// block in s, or -1 if not found.  The search is brace-aware so that a
// literal "}" inside the body would confuse it; we accept that limitation
// because real-world prompts don't contain bare braces.
func indexShellBlockEnd(s string) int {
	depth := 1
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// expandWithEscape expands {{args}} inside a shell block, shell-escaping
// the substitution so it can be embedded in a shell snippet.
func expandWithEscape(body, args string) string {
	const token = "{{args}}"
	if !strings.Contains(body, token) {
		return body
	}
	// Split on the token so we can escape just the inserted parts.
	parts := strings.Split(body, token)
	var b strings.Builder
	for k, p := range parts {
		b.WriteString(p)
		if k < len(parts)-1 {
			b.WriteString(ShellQuote(args))
		}
	}
	return b.String()
}

// ShellQuote returns a POSIX-shell-safe single-quoted form of s.
// Single quotes inside s are encoded as '\”', matching the standard
// trick:  'foo'\''bar'   →   "foo'bar".
//
// Empty string returns "" (two single quotes, which is an empty arg).
func ShellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SplitArgs tokenises a raw slash-command input.  Whitespace separates
// tokens; matching quotes group tokens.  Mirrors the lightweight shlex
// semantics the TUI uses elsewhere.
func SplitArgs(s string) []string {
	var out []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\'' && !inDouble:
			inSingle = !inSingle
		case c == '"' && !inSingle:
			inDouble = !inDouble
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// Sanity-check helper kept for future expansion.  It deliberately lives
// next to Expand so any template-syntax change happens in one place.
func validateTemplate(t string) error {
	if !strings.Contains(t, "{{args}}") && strings.Contains(t, "!{") {
		// A shell block without args is allowed but worth flagging.
		return fmt.Errorf("commands: shell block without {{args}}: %q", t)
	}
	return nil
}

var _ = validateTemplate // silence unused warning while keeping it available