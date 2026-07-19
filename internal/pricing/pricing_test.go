package pricing

import "testing"

func TestLookup(t *testing.T) {
	tests := []struct {
		name  string
		model string
		want  bool
	}{
		{"exact known model", "claude-opus-4-8", true},
		{"dated snapshot falls back by family", "claude-opus-4-5-20251101", true},
		{"sonnet family fallback", "claude-sonnet-4-6-20260101", true},
		{"haiku family fallback", "claude-haiku-4-5-20251001", true},
		{"local ollama model unknown", "gemma3:12b", false},
		{"empty model unknown", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := Lookup(tt.model)
			if ok != tt.want {
				t.Errorf("Lookup(%q) ok = %v, want %v", tt.model, ok, tt.want)
			}
		})
	}
}

func TestRatesCost(t *testing.T) {
	r := Rates{InputPerM: 5.00, OutputPerM: 25.00, CacheWritePerM: 6.25, CacheReadPerM: 0.50}

	got := r.Cost(1_000_000, 1_000_000, 1_000_000, 1_000_000)
	want := 5.00 + 25.00 + 0.50 + 6.25
	if got != want {
		t.Errorf("Cost at 1M each = %v, want %v", got, want)
	}

	if got := r.Cost(0, 0, 0, 0); got != 0 {
		t.Errorf("Cost of nothing = %v, want 0", got)
	}

	// 200k input tokens at $5/M = $1.00
	if got := r.Cost(200_000, 0, 0, 0); got != 1.00 {
		t.Errorf("Cost(200_000, 0, 0, 0) = %v, want 1.00", got)
	}
}
