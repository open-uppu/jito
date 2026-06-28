package commands

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpand_PlainArgs(t *testing.T) {
	got := Expand("hello {{args}}!", "world")
	assert.Equal(t, "hello world!", got)
}

func TestExpand_NoArgs(t *testing.T) {
	got := Expand("static template", "")
	assert.Equal(t, "static template", got)
}

func TestExpand_MultipleOccurrences(t *testing.T) {
	got := Expand("{{args}} - {{args}}", "x")
	assert.Equal(t, "x - x", got)
}

func TestExpand_ShellBlockEscapesArgs(t *testing.T) {
	tpl := "run: !{echo {{args}} | tee log}"
	got := Expand(tpl, "hello world")
	// Inside !{}, {{args}} must be single-quoted.
	assert.Equal(t, "run: !{echo 'hello world' | tee log}", got)
}

func TestExpand_ShellBlockWithSpecialChars(t *testing.T) {
	got := Expand("!{echo {{args}}}", `O'Reilly; rm -rf /`)
	// Single quotes inside the arg become '\''.
	assert.Equal(t, `!{echo 'O'\''Reilly; rm -rf /'}`, got)
}

func TestExpand_ShellBlockEmptyArgs(t *testing.T) {
	got := Expand("!{echo {{args}}}", "")
	// Empty arg → "''"
	assert.Equal(t, "!{echo ''}", got)
}

func TestExpand_PlainAndShellMixed(t *testing.T) {
	tpl := "raw={{args}}; shell=!{echo {{args}}}"
	got := Expand(tpl, "hi there")
	// !{...} braces are preserved; only {{args}} gets shell-escaped.
	assert.Equal(t, "raw=hi there; shell=!{echo 'hi there'}", got)
}

func TestExpand_UnmatchedShellBlock(t *testing.T) {
	tpl := "broken !{echo {{args}}"
	got := Expand(tpl, "x")
	assert.Equal(t, "broken !{echo {{args}}", got)
}

func TestExpand_NoArgsPlaceholder(t *testing.T) {
	got := Expand("hello world", "ignored")
	assert.Equal(t, "hello world", got)
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"":           "''",
		"hello":      "'hello'",
		"a b":        "'a b'",
		"O'Reilly":   `'O'\''Reilly'`,
		"$VAR":       "'$VAR'",
		"`backtick`": "'`backtick`'",
		"a\nb":       "'a\nb'",
		"$(rm -rf)":  "'$(rm -rf)'",
	}
	for in, want := range cases {
		assert.Equal(t, want, ShellQuote(in), in)
	}
}

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a b c", []string{"a", "b", "c"}},
		{"  a   b  ", []string{"a", "b"}},
		{`a "b c" d`, []string{"a", "b c", "d"}},
		{`a 'b c' d`, []string{"a", "b c", "d"}},
		{`a "b 'c' d" e`, []string{"a", "b 'c' d", "e"}},
		{`a "unterminated`, []string{"a", "unterminated"}},
		{"\t \t", nil},
	}
	for _, c := range cases {
		got := SplitArgs(c.in)
		assert.Equal(t, c.want, got, c.in)
	}
}

func TestExpand_EndToEndLikeSmokeTest(t *testing.T) {
	// This mirrors the smoke test in the task: `hello world` → "echo world".
	cmd := &Command{Slash: "/hello", Prompt: "echo {{args}}"}
	got := Expand(cmd.Prompt, "world")
	assert.Equal(t, "echo world", got)
}