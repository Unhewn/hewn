package slash

import (
	"context"
	"strings"
	"testing"
)

func echoCommand(name string) Command {
	return Command{
		Name:        name,
		Description: "echoes its args",
		Run: func(_ context.Context, _ *Context, args string) Result {
			return Result{Output: "args=" + args}
		},
	}
}

func TestRegistry_ListOrder(t *testing.T) {
	r := NewRegistry()
	r.Register(echoCommand("b"))
	r.Register(echoCommand("a"))

	list := r.List()
	if len(list) != 2 || list[0].Name != "b" || list[1].Name != "a" {
		t.Errorf("List() = %v, want [b a] in registration order", list)
	}

	// Re-registering the same name replaces in place without duplicating order.
	r.Register(Command{Name: "b", Description: "replaced"})
	list = r.List()
	if len(list) != 2 || list[0].Description != "replaced" {
		t.Errorf("List() after re-register = %+v", list)
	}
}

func TestDispatch_NonSlashLineIsUnhandled(t *testing.T) {
	r := NewRegistry()
	r.Register(echoCommand("help"))

	_, handled := r.Dispatch(context.Background(), &Context{}, "hello there")
	if handled {
		t.Error("Dispatch(non-slash line) handled = true, want false")
	}
}

func TestDispatch_KnownCommand(t *testing.T) {
	r := NewRegistry()
	r.Register(echoCommand("greet"))

	result, handled := r.Dispatch(context.Background(), &Context{}, "/greet world")
	if !handled {
		t.Fatal("Dispatch(/greet world) handled = false, want true")
	}
	if result.Output != "args=world" {
		t.Errorf("result.Output = %q, want %q", result.Output, "args=world")
	}
}

func TestDispatch_UnknownCommand(t *testing.T) {
	r := NewRegistry()

	result, handled := r.Dispatch(context.Background(), &Context{}, "/nope")
	if !handled {
		t.Fatal("Dispatch(/nope) handled = false, want true (unrecognized slash commands are still \"handled\")")
	}
	if !strings.Contains(result.Output, "unknown command") {
		t.Errorf("result.Output = %q, want it to mention an unknown command", result.Output)
	}
}

func TestDispatch_NoArgs(t *testing.T) {
	r := NewRegistry()
	r.Register(echoCommand("bare"))

	result, handled := r.Dispatch(context.Background(), &Context{}, "/bare")
	if !handled {
		t.Fatal("Dispatch(/bare) handled = false, want true")
	}
	if result.Output != "args=" {
		t.Errorf("result.Output = %q, want %q", result.Output, "args=")
	}
}

func TestDispatch_TrimsWhitespace(t *testing.T) {
	r := NewRegistry()
	r.Register(echoCommand("greet"))

	result, handled := r.Dispatch(context.Background(), &Context{}, "  /greet   world  ")
	if !handled {
		t.Fatal("Dispatch handled = false, want true")
	}
	if result.Output != "args=world" {
		t.Errorf("result.Output = %q, want %q (surrounding and internal extra whitespace trimmed)", result.Output, "args=world")
	}
}
