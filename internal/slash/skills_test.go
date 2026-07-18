package slash

import (
	"context"
	"strings"
	"testing"

	"github.com/unhewn/hewn/internal/skill"
)

func TestRegisterSkills_ActivatesPromptAndTools(t *testing.T) {
	c := newTestContext(t)
	c.Loop.Tools = c.Tools // mirror real wiring: buildLoop sets both to the same registry.

	skills := []skill.Skill{
		{Name: "reviewer", Description: "review code", Tools: []string{"read"}, Prompt: "You review code."},
	}
	warnings := RegisterSkills(c.Registry, skills, c.Tools)
	if len(warnings) != 0 {
		t.Fatalf("RegisterSkills() warnings = %v, want none", warnings)
	}

	result, handled := c.Registry.Dispatch(context.Background(), c, "/reviewer")
	if !handled {
		t.Fatal("Dispatch(/reviewer) handled = false")
	}
	if !strings.Contains(result.Output, "reviewer") {
		t.Errorf("output = %q, want it to mention the skill name", result.Output)
	}
	if c.Loop.System != "You review code." {
		t.Errorf("Loop.System = %q", c.Loop.System)
	}

	list := c.Loop.Tools.List()
	if len(list) != 1 || list[0].Name() != "read" {
		t.Errorf("Loop.Tools after activation = %v, want just [read]", list)
	}
	toolsList := c.Tools.List()
	if len(toolsList) != 1 || toolsList[0].Name() != "read" {
		t.Errorf("Context.Tools after activation = %v, want just [read] (kept in sync with Loop.Tools)", toolsList)
	}
}

func TestRegisterSkills_NoToolsMeansNoRestriction(t *testing.T) {
	c := newTestContext(t)
	c.Loop.Tools = c.Tools
	fullCount := len(c.Tools.List())

	skills := []skill.Skill{{Name: "writer", Prompt: "Be a writer."}}
	if warnings := RegisterSkills(c.Registry, skills, c.Tools); len(warnings) != 0 {
		t.Fatalf("RegisterSkills() warnings = %v", warnings)
	}

	c.Registry.Dispatch(context.Background(), c, "/writer")
	if len(c.Loop.Tools.List()) != fullCount {
		t.Errorf("Loop.Tools after a no-tools skill = %d tools, want unchanged %d", len(c.Loop.Tools.List()), fullCount)
	}
}

func TestRegisterSkills_SkipsCollisionWithBuiltin(t *testing.T) {
	c := newTestContext(t)

	skills := []skill.Skill{{Name: "help", Prompt: "pretend to be help"}}
	warnings := RegisterSkills(c.Registry, skills, c.Tools)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "collides") {
		t.Fatalf("warnings = %v, want one mentioning a collision", warnings)
	}

	result, _ := c.Registry.Dispatch(context.Background(), c, "/help")
	if strings.Contains(result.Output, "pretend to be help") {
		t.Error("/help was overridden by a colliding skill, want the built-in to win")
	}
}

func TestRegisterSkills_SkipsUnknownTool(t *testing.T) {
	c := newTestContext(t)

	skills := []skill.Skill{{Name: "ghost", Tools: []string{"does-not-exist"}, Prompt: "boo"}}
	warnings := RegisterSkills(c.Registry, skills, c.Tools)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "unknown tool") {
		t.Fatalf("warnings = %v, want one mentioning the unknown tool", warnings)
	}

	for _, cmd := range c.Registry.List() {
		if cmd.Name == "ghost" {
			t.Fatal("skill with an unknown tool was registered anyway")
		}
	}
}
