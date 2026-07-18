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

func TestNewSubset(t *testing.T) {
	base := NewRegistry()
	base.Register(fakeTool{name: "read", risk: RiskReadOnly})
	base.Register(fakeTool{name: "write", risk: RiskMutating})
	base.Register(fakeTool{name: "bash", risk: RiskArbitrary})

	t.Run("requested order, not base order", func(t *testing.T) {
		sub, err := NewSubset(base, []string{"bash", "read"})
		if err != nil {
			t.Fatalf("NewSubset() error = %v", err)
		}
		list := sub.List()
		if len(list) != 2 || list[0].Name() != "bash" || list[1].Name() != "read" {
			t.Errorf("List() = %v, want [bash read]", list)
		}
	})

	t.Run("unknown name errors", func(t *testing.T) {
		if _, err := NewSubset(base, []string{"read", "missing"}); err == nil {
			t.Fatal("NewSubset() error = nil, want error for unknown tool")
		}
	})

	t.Run("subset is independent of base", func(t *testing.T) {
		sub, err := NewSubset(base, []string{"read"})
		if err != nil {
			t.Fatalf("NewSubset() error = %v", err)
		}
		sub.Register(fakeTool{name: "extra", risk: RiskReadOnly})
		if _, ok := base.Get("extra"); ok {
			t.Error("Register on subset leaked into base")
		}
	})
}
