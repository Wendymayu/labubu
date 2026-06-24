// Package pricing provides model pricing cost calculation functions.
// These are pure functions that operate on storage types without
// depending on the storage implementation.
package pricing

import (
	"math"

	"github.com/labubu/labubu/internal/storage"
)

// CalculateSpanCost computes the monetary cost for a single span.
// Returns nil if the span has no tokens or the model has no pricing configured.
func CalculateSpanCost(span storage.Span, pricings []storage.ModelPricing) *float64 {
	if span.TotalTokens == nil || *span.TotalTokens == 0 {
		return nil
	}
	if span.GenAIRequestModel == nil || *span.GenAIRequestModel == "" {
		return nil
	}

	// Anthropic prompt-caching charges cache tokens at differential rates:
	// cache creation (write) = 1.25× input price, cache read = 0.1× input price.
	const cacheCreateRate = 1.25
	const cacheReadRate = 0.1

	modelName := *span.GenAIRequestModel
	for _, p := range pricings {
		if p.ModelName == modelName {
			inputTokens := float64(0)
			outputTokens := float64(0)
			cacheCreate := float64(0)
			cacheRead := float64(0)
			if span.InputTokens != nil {
				inputTokens = float64(*span.InputTokens)
			}
			if span.OutputTokens != nil {
				outputTokens = float64(*span.OutputTokens)
			}
			if span.CacheCreationTokens != nil {
				cacheCreate = float64(*span.CacheCreationTokens)
			}
			if span.CacheReadTokens != nil {
				cacheRead = float64(*span.CacheReadTokens)
			}
			cost := (inputTokens*p.InputPrice +
				cacheCreate*p.InputPrice*cacheCreateRate +
				cacheRead*p.InputPrice*cacheReadRate +
				outputTokens*p.OutputPrice) / 1_000_000.0
			cost = math.Round(cost*1_000_000) / 1_000_000
			return &cost
		}
	}
	return nil
}

// CalculateTraceCost computes total cost for a trace from its spans.
// Returns total cost (nil if none), currency string, and count of unpriced spans.
func CalculateTraceCost(spans []storage.Span, pricings []storage.ModelPricing) (cost *float64, currency string, unpriced int) {
	var total float64
	hasCost := false

	for _, span := range spans {
		spanCost := CalculateSpanCost(span, pricings)
		if spanCost != nil {
			total += *spanCost
			hasCost = true
			if currency == "" {
				for _, p := range pricings {
					if span.GenAIRequestModel != nil && *span.GenAIRequestModel == p.ModelName {
						currency = p.Currency
						break
					}
				}
			}
		} else if span.TotalTokens != nil && *span.TotalTokens > 0 {
			unpriced++
		}
	}

	if !hasCost {
		return nil, "", unpriced
	}
	c := math.Round(total*1_000_000) / 1_000_000
	return &c, currency, unpriced
}
