package commands

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func mkCmd(slash string) *Command {
	return &Command{Slash: slash, Name: slash[1:], Description: slash, Prompt: slash, Source: SourceGlobal}
}

func TestRegistry_UpdateAndGet(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{mkCmd("/hello"), mkCmd("/bye")}, nil)

	c, ok := r.Get("/hello")
	require.True(t, ok)
	assert.Equal(t, "hello", c.Name)
	assert.Equal(t, 2, r.Count())
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry()
	_, ok := r.Get("/nope")
	assert.False(t, ok)
}

func TestRegistry_AllOrderStable(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{mkCmd("/z"), mkCmd("/a"), mkCmd("/m")}, nil)
	all := r.All()
	require.Len(t, all, 3)
	assert.Equal(t, "/a", all[0].Slash)
	assert.Equal(t, "/m", all[1].Slash)
	assert.Equal(t, "/z", all[2].Slash)
}

func TestRegistry_Lookup(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/git:commit"),
		mkCmd("/git:status"),
		mkCmd("/test"),
	}, nil)

	d, cs := r.Lookup("/git:commit hello world")
	assert.Equal(t, DecisionExact, d)
	require.Len(t, cs, 1)
	assert.Equal(t, "/git:commit", cs[0].Slash)

	d, cs = r.Lookup("/git:")
	assert.Equal(t, DecisionAmbiguous, d)
	assert.Len(t, cs, 2)

	d, _ = r.Lookup("/nope")
	assert.Equal(t, DecisionUnknown, d)

	d, _ = r.Lookup("not a slash")
	assert.Equal(t, DecisionUnknown, d)

	d, _ = r.Lookup("/help")
	assert.Equal(t, DecisionBuiltin, d)

	d, cs = r.Lookup("/test only one")
	assert.Equal(t, DecisionExact, d)
	assert.Equal(t, "/test", cs[0].Slash)
}

func TestRegistry_PrefixMatch(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/git:commit"),
		mkCmd("/git:status"),
		mkCmd("/ls"),
	}, nil)
	got := r.PrefixMatch("/git:")
	require.Len(t, got, 2)
	slashes := []string{got[0].Slash, got[1].Slash}
	sort.Strings(slashes)
	assert.Equal(t, []string{"/git:commit", "/git:status"}, slashes)

	assert.Empty(t, r.PrefixMatch("/zzz"))
}

func TestRegistry_FuzzyMatch_Empty(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/hello"),
		mkCmd("/help"),
		mkCmd("/zap"),
	}, nil)
	hits := r.FuzzyMatch("", 0)
	assert.Len(t, hits, 3)
}

func TestRegistry_FuzzyMatch_PrefixBonus(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/hello"),
		mkCmd("/help"),
		mkCmd("/zap"),
	}, nil)
	hits := r.FuzzyMatch("/hel", 0)
	require.Len(t, hits, 2)
	// Both /hello and /help start with /hel → +100 prefix bonus +30
	// match score. Length penalty then ranks /help (5 runes) above
	// /hello (6 runes); same score → lex order, but score wins first.
	assert.Equal(t, "/help", hits[0].Cmd.Slash)
	assert.Equal(t, "/hello", hits[1].Cmd.Slash)
}

func TestRegistry_FuzzyMatch_Subsequence(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/git:commit"),
		mkCmd("/git:status"),
		mkCmd("/hello"),
	}, nil)
	hits := r.FuzzyMatch("/gc", 0)
	require.Len(t, hits, 1)
	assert.Equal(t, "/git:commit", hits[0].Cmd.Slash)
	// 'g' at index 1, 'c' at index 5 (in "/git:commit")
	assert.Equal(t, []int{1, 5}, hits[0].Pos)
}

func TestRegistry_FuzzyMatch_NoMatch(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{mkCmd("/hello")}, nil)
	hits := r.FuzzyMatch("/xyz", 0)
	assert.Empty(t, hits)
}

func TestRegistry_FuzzyMatch_Limit(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/aaa"),
		mkCmd("/aab"),
		mkCmd("/aac"),
	}, nil)
	hits := r.FuzzyMatch("/a", 2)
	assert.Len(t, hits, 2)
}

func TestRegistry_FilterByQuery(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		mkCmd("/hello"),
		mkCmd("/help"),
		mkCmd("/world"),
	}, nil)
	out := r.FilterByQuery("/hel")
	require.Len(t, out, 2)
	slashes := []string{out[0].Slash, out[1].Slash}
	sort.Strings(slashes)
	assert.Equal(t, []string{"/hello", "/help"}, slashes)
}

func TestRegistry_String(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{
		{Slash: "/a", Name: "a", Description: "alpha", Prompt: "p", Source: SourceGlobal},
		{Slash: "/b", Name: "b", Description: "beta", Prompt: "p", Source: SourceProject},
	}, nil)
	s := r.String()
	assert.Contains(t, s, "/a")
	assert.Contains(t, s, "alpha")
	assert.Contains(t, s, "[project]")
}

func TestRegistry_Errors(t *testing.T) {
	r := NewRegistry()
	r.Update(nil, []error{assert.AnError})
	errs := r.Errors()
	require.Len(t, errs, 1)
}

func TestRegistry_LoadFromDirs(t *testing.T) {
	g := t.TempDir()
	p := t.TempDir()
	writeTOML(t, g, "g.toml", `description = "g"`+"\nprompt = \"g\"\n")
	writeTOML(t, p, "p.toml", `description = "p"`+"\nprompt = \"p\"\n")

	r := NewRegistry()
	errs := r.LoadFromDirs(g, p)
	require.Empty(t, errs)
	assert.Equal(t, 2, r.Count())
}

func TestRegistry_LoadFromDirs_WithErrors(t *testing.T) {
	g := t.TempDir()
	p := t.TempDir()
	writeTOML(t, g, "good.toml", `description = "g"`+"\nprompt = \"g\"\n")
	writeTOML(t, g, "bad.toml", `description = "b"`+"\n") // no prompt

	r := NewRegistry()
	errs := r.LoadFromDirs(g, p)
	require.Len(t, errs, 1)
	assert.Equal(t, 1, r.Count(), "good file should still be loaded")
}

func TestRegistry_Concurrent(t *testing.T) {
	r := NewRegistry()
	r.Update([]*Command{mkCmd("/a"), mkCmd("/b")}, nil)

	done := make(chan struct{}, 10)
	for i := 0; i < 5; i++ {
		go func() {
			_ = r.All()
			done <- struct{}{}
		}()
		go func() {
			r.Update([]*Command{mkCmd("/c")}, nil)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 10; i++ {
		<-done
	}
	// Final state should be consistent.
	assert.GreaterOrEqual(t, r.Count(), 1)
}

func TestIsBuiltin(t *testing.T) {
	assert.True(t, IsBuiltin("/help"))
	assert.True(t, IsBuiltin("/clear"))
	assert.True(t, IsBuiltin("/quit"))
	assert.False(t, IsBuiltin("/custom"))
	assert.False(t, IsBuiltin("help"))
}