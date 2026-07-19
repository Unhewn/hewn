// Package pricing converts token counts into dollar costs for known
// hosted models. It knows nothing about providers, requests, or usage
// tracking -- callers hand it plain token counts and a model ID.
package pricing

import "strings"

// Rates is one model's price per million tokens, by token category.
// CacheWritePerM is the 5-minute-TTL write rate (Anthropic's more
// expensive 1-hour TTL is not distinguished -- Hewn's own cache
// breakpoints always use the default ephemeral TTL).
type Rates struct {
	InputPerM      float64
	OutputPerM     float64
	CacheWritePerM float64
	CacheReadPerM  float64
}

// Cost prices one turn's usage under these rates.
func (r Rates) Cost(inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int) float64 {
	const perMillion = 1_000_000
	return float64(inputTokens)*r.InputPerM/perMillion +
		float64(outputTokens)*r.OutputPerM/perMillion +
		float64(cacheReadTokens)*r.CacheReadPerM/perMillion +
		float64(cacheWriteTokens)*r.CacheWritePerM/perMillion
}

// table holds published rates for known hosted models, keyed by exact
// model ID. Update by hand when Anthropic changes pricing -- there is no
// live pricing endpoint to read this from.
var table = map[string]Rates{
	"claude-opus-4-8":   {InputPerM: 5.00, OutputPerM: 25.00, CacheWritePerM: 6.25, CacheReadPerM: 0.50},
	"claude-opus-4-7":   {InputPerM: 5.00, OutputPerM: 25.00, CacheWritePerM: 6.25, CacheReadPerM: 0.50},
	"claude-opus-4-6":   {InputPerM: 5.00, OutputPerM: 25.00, CacheWritePerM: 6.25, CacheReadPerM: 0.50},
	"claude-opus-4-5":   {InputPerM: 5.00, OutputPerM: 25.00, CacheWritePerM: 6.25, CacheReadPerM: 0.50},
	"claude-sonnet-5":   {InputPerM: 3.00, OutputPerM: 15.00, CacheWritePerM: 3.75, CacheReadPerM: 0.30},
	"claude-sonnet-4-6": {InputPerM: 3.00, OutputPerM: 15.00, CacheWritePerM: 3.75, CacheReadPerM: 0.30},
	"claude-haiku-4-5":  {InputPerM: 1.00, OutputPerM: 5.00, CacheWritePerM: 1.25, CacheReadPerM: 0.10},
}

// prefixFallback maps a model-ID substring to its family's rates, for IDs
// not in table verbatim -- dated snapshots (claude-opus-4-5-20251101) and
// any future point release Hewn hasn't been updated for yet. Checked in
// order; first match wins, so more specific tiers must precede
// less-specific ones sharing a substring.
var prefixFallback = []struct {
	substr string
	rates  Rates
}{
	{"opus", table["claude-opus-4-8"]},
	{"sonnet", table["claude-sonnet-5"]},
	{"haiku", table["claude-haiku-4-5"]},
}

// Lookup returns the rates for model, or ok=false if model isn't a known
// hosted Anthropic model -- local/self-hosted models (Ollama, llama.cpp)
// have no meaningful $ cost and correctly report unknown rather than a
// fabricated zero that looks like a real "it's free" answer.
func Lookup(model string) (Rates, bool) {
	if r, ok := table[model]; ok {
		return r, true
	}
	lower := strings.ToLower(model)
	for _, f := range prefixFallback {
		if strings.Contains(lower, f.substr) {
			return f.rates, true
		}
	}
	return Rates{}, false
}
