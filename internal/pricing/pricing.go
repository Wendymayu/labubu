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

	modelName := *span.GenAIRequestModel
	for _, p := range pricings {
		if p.ModelName == modelName {
			inputTokens := float64(0)
			outputTokens := float64(0)
			if span.InputTokens != nil {
				inputTokens = float64(*span.InputTokens)
			}
			if span.OutputTokens != nil {
				outputTokens = float64(*span.OutputTokens)
			}
			cost := (inputTokens*p.InputPrice + outputTokens*p.OutputPrice) / 1_000_000.0
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
