package tui

import (
	"strings"
	"testing"
)

func TestFormatContextTokens_UnknownWindowShowsBareCount(t *testing.T) {
	got := formatContextTokens(12400, 0)
	if got != "12.4k tok" {
		t.Errorf("formatContextTokens(12400, 0) = %q, want %q", got, "12.4k tok")
	}
}

func TestFormatContextTokens_KnownWindowShowsPercentage(t *testing.T) {
	got := formatContextTokens(12400, 128000)
	want := "12.4k / 128k tok (10%)"
	if got != want {
		t.Errorf("formatContextTokens(12400, 128000) = %q, want %q", got, want)
	}
}

func TestRenderStatusBar_WarnsPastThreshold(t *testing.T) {
	under := renderStatusBar(80, "m", "/cwd", 50000, 100000, 0, stateIdle, "") // 50%
	over := renderStatusBar(80, "m", "/cwd", 80000, 100000, 0, stateIdle, "")  // 80%
	unknown := renderStatusBar(80, "m", "/cwd", 80000, 0, 0, stateIdle, "")    // no window configured

	if under == over {
		t.Error("status bar rendered identically under and over the warning threshold -- color branch never took effect")
	}
	if unknown == over {
		t.Error("an unconfigured context window should never trigger the warning color")
	}
}

func TestRenderStatusBar_ShowsCumulativeSessionTotal(t *testing.T) {
	bar := renderStatusBar(80, "m", "/cwd", 0, 0, 42100, stateIdle, "")
	if !strings.Contains(bar, "Σ42.1k") {
		t.Errorf("status bar = %q, want it to show the cumulative session total (Σ42.1k)", bar)
	}
}
