package tui

import (
	"context"
	"testing"

	"github.com/unhewn/hewn/internal/slash"
)

func TestLongestCommonPrefix_SingleMatch(t *testing.T) {
	if got := longestCommonPrefix([]string{"model"}); got != "model" {
		t.Errorf("longestCommonPrefix([model]) = %q, want %q", got, "model")
	}
}

func TestLongestCommonPrefix_SharedPrefix(t *testing.T) {
	if got := longestCommonPrefix([]string{"model", "models"}); got != "model" {
		t.Errorf("longestCommonPrefix([model, models]) = %q, want %q", got, "model")
	}
}

func TestLongestCommonPrefix_NoSharedPrefix(t *testing.T) {
	if got := longestCommonPrefix([]string{"model", "cost"}); got != "" {
		t.Errorf("longestCommonPrefix([model, cost]) = %q, want empty", got)
	}
}

func TestCompleteSlashCommand_SingleMatchAddsTrailingSpace(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/mod")

	m.completeSlashCommand()

	if got := m.input.Value(); got != "/model " {
		t.Errorf("input = %q, want %q (unique match gets a trailing space, ready for args)", got, "/model ")
	}
}

func TestCompleteSlashCommand_AmbiguousStopsAtCommonPrefix(t *testing.T) {
	reg := slash.NewRegistry()
	reg.Register(slash.Command{Name: "model", Run: func(context.Context, *slash.Context, string) slash.Result { return slash.Result{} }})
	reg.Register(slash.Command{Name: "models", Run: func(context.Context, *slash.Context, string) slash.Result { return slash.Result{} }})

	m := newTestModel(t)
	m.slashRegistry = reg
	m.input.SetValue("/mo")

	m.completeSlashCommand()

	if got := m.input.Value(); got != "/model" {
		t.Errorf("input = %q, want %q (two matches share this prefix -- no trailing space, since it isn't unambiguous)", got, "/model")
	}
}

func TestCompleteSlashCommand_NoMatchIsANoOp(t *testing.T) {
	m := newTestModel(t)
	m.input.SetValue("/zzz")

	m.completeSlashCommand()

	if got := m.input.Value(); got != "/zzz" {
		t.Errorf("input = %q, want unchanged %q", got, "/zzz")
	}
}
