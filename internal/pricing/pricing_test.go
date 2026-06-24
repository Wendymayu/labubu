package pricing

import (
	"math"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func u32Ptr(v uint32) *uint32 { return &v }
func sPtr(v string) *string   { return &v }

func floatEqual(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestCalculateSpanCostCacheTokens(t *testing.T) {
	pricings := []storage.ModelPricing{
		{ModelName: "claude-opus-4-8", InputPrice: 15.0, OutputPrice: 75.0, Currency: "USD"},
	}

	tests := []struct {
		name   string
		span   storage.Span
		expect float64
	}{
		{
			name: "cache tokens priced at differential rates",
			// (2*15 + 189194*15*1.25 + 5000*15*0.1 + 100*75)/1e6 = 3.562418
			span: storage.Span{
				InputTokens:         u32Ptr(2),
				OutputTokens:        u32Ptr(100),
				CacheCreationTokens: u32Ptr(189194),
				CacheReadTokens:     u32Ptr(5000),
				TotalTokens:         u32Ptr(194296),
				GenAIRequestModel:   sPtr("claude-opus-4-8"),
			},
			expect: 3.562418,
		},
		{
			name: "cache read priced at 0.1x",
			// (100000*15*0.1)/1e6 = 0.15
			span: storage.Span{
				CacheReadTokens:   u32Ptr(100000),
				TotalTokens:       u32Ptr(100000),
				GenAIRequestModel: sPtr("claude-opus-4-8"),
			},
			expect: 0.15,
		},
		{
			name: "no cache tokens — backwards compatible",
			// (100*15 + 50*75)/1e6 = 0.00525
			span: storage.Span{
				InputTokens:       u32Ptr(100),
				OutputTokens:      u32Ptr(50),
				TotalTokens:       u32Ptr(150),
				GenAIRequestModel: sPtr("claude-opus-4-8"),
			},
			expect: 0.00525,
		},
		{
			name: "unpriced model returns nil",
			span: storage.Span{
				InputTokens:       u32Ptr(100),
				TotalTokens:       u32Ptr(100),
				GenAIRequestModel: sPtr("unknown-model"),
			},
			expect: -1, // sentinel: nil expected
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CalculateSpanCost(tt.span, pricings)
			if tt.expect < 0 {
				if got != nil {
					t.Errorf("expected nil cost, got %v", *got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected cost %v, got nil", tt.expect)
			}
			if !floatEqual(*got, tt.expect) {
				t.Errorf("cost: got %v, want %v", *got, tt.expect)
			}
		})
	}
}
