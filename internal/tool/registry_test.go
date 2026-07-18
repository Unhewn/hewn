package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeTool struct {
	name string
	risk RiskLevel
}

func (f fakeTool) Name() string            { return f.name }
func (f fakeTool) Description() string     { return "fake" }
func (f fakeTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (f fakeTool) Risk() RiskLevel         { return f.risk }
func (f fakeTool) Execute(context.Context, json.RawMessage, IO) (Result, error) {
	return Result{}, nil
}

func TestRegistry(t *testing.T) {
	r := NewRegistry()
	r.Register(fakeTool{name: "read", risk: RiskReadOnly})
	r.Register(fakeTool{name: "bash", risk: RiskArbitrary})

	if _, ok := r.Get("missing"); ok {
		t.Error("Get(missing) = ok, want not found")
	}

	got, ok := r.Get("bash")
	if !ok || got.Name() != "bash" {
		t.Errorf("Get(bash) = %v, %v", got, ok)
	}

	list := r.List()
	if len(list) != 2 || list[0].Name() != "read" || list[1].Name() != "bash" {
		t.Errorf("List() = %v, want [read bash] in registration order", list)
	}

	// Re-registering the same name replaces in place without duplicating order.
	r.Register(fakeTool{name: "read", risk: RiskMutating})
	list = r.List()
	if len(list) != 2 {
		t.Fatalf("List() after re-register = %v, want length 2", list)
	}
	if list[0].Risk() != RiskMutating {
		t.Errorf("re-registered read Risk() = %v, want RiskMutating", list[0].Risk())
	}
}
