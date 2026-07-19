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
	under := renderStatusBar(80, "m", "/cwd", 50000, 100000, 0, 0, false, stateIdle, "") // 50%
	over := renderStatusBar(80, "m", "/cwd", 80000, 100000, 0, 0, false, stateIdle, "")  // 80%
	unknown := renderStatusBar(80, "m", "/cwd", 80000, 0, 0, 0, false, stateIdle, "")    // no window configured

	if under == over {
		t.Error("status bar rendered identically under and over the warning threshold -- color branch never took effect")
	}
	if unknown == over {
		t.Error("an unconfigured context window should never trigger the warning color")
	}
}

func TestRenderStatusBar_ShowsCumulativeSessionTotal(t *testing.T) {
	bar := renderStatusBar(80, "m", "/cwd", 0, 0, 42100, 0, false, stateIdle, "")
	if !strings.Contains(bar, "Σ42.1k") {
		t.Errorf("status bar = %q, want it to show the cumulative session total (Σ42.1k)", bar)
	}
}

func TestRenderStatusBar_ShowsCostWhenKnown(t *testing.T) {
	known := renderStatusBar(80, "m", "/cwd", 0, 0, 1000, 0.0842, true, stateIdle, "")
	if !strings.Contains(known, "$0.0842") {
		t.Errorf("status bar = %q, want it to show the known cumulative cost ($0.0842)", known)
	}

	unknown := renderStatusBar(80, "m", "/cwd", 0, 0, 1000, 0, false, stateIdle, "")
	if strings.Contains(unknown, "$") {
		t.Errorf("status bar = %q, want no $ figure when cost is unknown (local/unpriced model)", unknown)
	}
}
