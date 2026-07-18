package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"
)

// waitDelay bounds how long Command's Wait will block on I/O after the
// process is killed. Without it, a shell command that has spawned a
// grandchild (e.g. "sh -c 'sleep 10'") can hold this process's stdout/stderr
// pipes open long after the shell itself has been killed, and Wait blocks
// on that orphaned grandchild instead of returning promptly on
// cancellation. This bounds the wait; it does not kill the grandchild
// (real process-group teardown is POSIX/Windows-divergent and deferred).
const waitDelay = 2 * time.Second

// envDenyPrefixes and envDenySuffixes are stripped from a subprocess's
// environment unless explicitly kept (HEWN.md §3 sandboxing: "inherits env
// minus a denylist ... except the one in use").
var (
	envDenyPrefixes = []string{"AWS_"}
	envDenySuffixes = []string{"_TOKEN", "_KEY"}
)

// FilterEnv returns environ with denylisted variables removed, except any
// named in keep (case-sensitive, e.g. the API key of the provider actually
// in use).
func FilterEnv(environ, keep []string) []string {
	kept := make(map[string]bool, len(keep))
	for _, k := range keep {
		kept[k] = true
	}

	filtered := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		if kept[name] || !denied(name) {
			filtered = append(filtered, kv)
		}
	}
	return filtered
}

func denied(name string) bool {
	upper := strings.ToUpper(name)
	for _, p := range envDenyPrefixes {
		if strings.HasPrefix(upper, p) {
			return true
		}
	}
	for _, suf := range envDenySuffixes {
		if strings.HasSuffix(upper, suf) {
			return true
		}
	}
	return false
}

// Command builds an *exec.Cmd for script, run through a shell, with its
// working directory pinned to the sandbox root and its environment
// filtered through FilterEnv. This is not a sandbox for the subprocess
// itself -- approval prompts are the real control (HEWN.md §3).
//
// Shell invocation is centralized here so a non-POSIX implementation can
// replace it in one place later (AGENTS.md: Windows is not supported yet).
func (s *Sandbox) Command(ctx context.Context, script string, keepEnv []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "sh", "-c", script)
	cmd.Dir = s.rootPath
	cmd.Env = FilterEnv(os.Environ(), keepEnv)
	cmd.WaitDelay = waitDelay
	return cmd
}
