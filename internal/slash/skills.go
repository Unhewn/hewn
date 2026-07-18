package slash

import (
	"context"
	"fmt"

	"github.com/unhewn/hewn/internal/skill"
	"github.com/unhewn/hewn/internal/tool"
)

// RegisterSkills turns each loaded skill into a Command and adds it to
// reg. A skill is skipped -- and reported in the returned warnings -- if
// its name collides with an already-registered command, or its tools list
// names a tool absent from fullTools. skill.Load already guarantees valid,
// unique-within-its-directory names, so this only has to guard against
// collisions with built-ins and bad tool references, both of which need
// reg/fullTools to detect.
func RegisterSkills(reg *Registry, skills []skill.Skill, fullTools *tool.Registry) []string {
	existing := map[string]bool{}
	for _, c := range reg.List() {
		existing[c.Name] = true
	}

	var warnings []string
	for _, sk := range skills {
		if existing[sk.Name] {
			warnings = append(warnings, fmt.Sprintf("skill %q: collides with an existing command, skipped", sk.Name))
			continue
		}

		toolsForSkill := fullTools
		if len(sk.Tools) > 0 {
			subset, err := tool.NewSubset(fullTools, sk.Tools)
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("skill %q: %v, skipped", sk.Name, err))
				continue
			}
			toolsForSkill = subset
		}

		reg.Register(skillCommand(sk, toolsForSkill))
		existing[sk.Name] = true
	}
	return warnings
}

// skillCommand builds the Command that activates sk: it sets the loop's
// system prompt and tool registry for subsequent turns, the same
// persist-until-changed convention modelCommand already uses for /model.
func skillCommand(sk skill.Skill, tools *tool.Registry) Command {
	return Command{
		Name:        sk.Name,
		Description: sk.Description,
		Run: func(_ context.Context, c *Context, _ string) Result {
			c.Loop.System = sk.Prompt
			c.Loop.Tools = tools
			// c.Tools is a separate field from c.Loop.Tools -- toolsCommand
			// (/tools) reads it, not c.Loop.Tools, so it must be kept in
			// sync or a skill's restriction wouldn't show up there.
			c.Tools = tools
			return Result{Output: fmt.Sprintf("skill activated: %s", sk.Name)}
		},
	}
}
