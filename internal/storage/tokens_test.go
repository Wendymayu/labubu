package storage

import "testing"

func TestDeriveTokenBuckets(t *testing.T) {
	u := func(v uint32) *uint32 { return &v }
	tests := []struct {
		name                 string
		attrs                map[string]string
		in, out, cc, cr, tot *uint32
	}{
		{
			name: "claude-code style: input non-cached, cache separate",
			attrs: map[string]string{
				"gen_ai.usage.input_tokens":                "2",
				"gen_ai.usage.output_tokens":               "100",
				"gen_ai.usage.cache_creation_input_tokens": "189194",
				"gen_ai.usage.cache_read_input_tokens":     "5000",
			},
			in: u(2), out: u(100), cc: u(189194), cr: u(5000), tot: u(194296),
		},
		{
			name:  "jiuwenclaw style: no cache, fallback keys",
			attrs: map[string]string{"input_tokens": "13011", "output_tokens": "53"},
			in:    u(13011), out: u(53), cc: nil, cr: nil, tot: u(13064),
		},
		{
			name: "self-reported total_tokens is IGNORED",
			attrs: map[string]string{
				"gen_ai.usage.input_tokens":  "100",
				"gen_ai.usage.output_tokens": "50",
				"gen_ai.usage.total_tokens":  "999",
			},
			in: u(100), out: u(50), cc: nil, cr: nil, tot: u(150),
		},
		{
			name:  "no token keys -> all nil",
			attrs: map[string]string{"other": "x"},
		},
		{
			name:  "nil map -> all nil",
			attrs: nil,
		},
		{
			name:  "malformed value falls through to fallback",
			attrs: map[string]string{"gen_ai.usage.input_tokens": "abc", "input_tokens": "42"},
			in:    u(42), tot: u(42),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in, out, cc, cr, tot := DeriveTokenBuckets(tt.attrs)
			if !u32eq(in, tt.in) {
				t.Errorf("input: got %v want %v", in, tt.in)
			}
			if !u32eq(out, tt.out) {
				t.Errorf("output: got %v want %v", out, tt.out)
			}
			if !u32eq(cc, tt.cc) {
				t.Errorf("cacheCreation: got %v want %v", cc, tt.cc)
			}
			if !u32eq(cr, tt.cr) {
				t.Errorf("cacheRead: got %v want %v", cr, tt.cr)
			}
			if !u32eq(tot, tt.tot) {
				t.Errorf("total: got %v want %v", tot, tt.tot)
			}
		})
	}
}

func u32eq(a, b *uint32) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}
