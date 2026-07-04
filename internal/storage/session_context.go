package storage

import "sort"

// SessionContextSpan is an LLM span belonging to the main agent of a session,
// with the token breakdown needed to render the session context bar chart.
type SessionContextSpan struct {
	TraceIDHex          string  `json:"trace_id_hex"`
	SpanIDHex           string  `json:"span_id"`
	Name                string  `json:"name"`
	StartTimeMS         uint64  `json:"start_time_ms"`
	InputTokens         *uint32 `json:"input_tokens"`
	OutputTokens        *uint32 `json:"output_tokens"`
	TotalTokens         *uint32 `json:"total_tokens"`
	CacheReadTokens     *uint32 `json:"cache_read_tokens"`
	CacheCreationTokens *uint32 `json:"cache_creation_tokens"`
	GenAIRequestModel   *string `json:"gen_ai_request_model"`
}

// sessionSpanInput is the internal span representation collected by each Store
// implementation before filtering. Uses hex string IDs so all backends (SQLite
// hex columns, chDB toHex(), memstore SpanIDToHex) feed the same pure logic
// without [8]byte decoding divergence.
type sessionSpanInput struct {
	TraceIDHex          string
	SpanIDHex           string
	ParentSpanIDHex     string
	Name                string
	StartTimeMS         uint64
	InputTokens         *uint32
	OutputTokens        *uint32
	TotalTokens         *uint32
	CacheReadTokens     *uint32
	CacheCreationTokens *uint32
	GenAIRequestModel   *string
}

// computeSessionContextSpans filters the main agent's LLM spans for a session.
//
// A span is "main agent" iff the nearest `.invoke` ancestor found by walking
// the parent chain equals its trace's root span. Subagent LLM spans (whose
// nearest `.invoke` ancestor is a nested subagent.invoke / agent.invoke) are
// excluded — their context resets on dispatch and must not be merged into the
// main session's trajectory.
//
// Pure function shared by all Store implementations. rootByTrace maps
// trace_id_hex -> root_span_id_hex.
func computeSessionContextSpans(rootByTrace map[string]string, spans []sessionSpanInput) []SessionContextSpan {
	byID := make(map[string]*sessionSpanInput, len(spans))
	for i := range spans {
		byID[spans[i].SpanIDHex] = &spans[i]
	}

	isInvoke := func(name string) bool {
		// Matches jiuwenclaw.agent.invoke / jiuwenclaw.subagent.invoke etc.
		for i := 0; i+len(".invoke") <= len(name); i++ {
			if name[i:i+len(".invoke")] == ".invoke" {
				return true
			}
		}
		return false
	}

	// ownerInvoke walks the parent chain to the nearest `.invoke` span.
	ownerInvoke := func(s *sessionSpanInput) *sessionSpanInput {
		cur := s
		for guard := 0; cur != nil && guard < 500; guard++ {
			if isInvoke(cur.Name) {
				return cur
			}
			if cur.ParentSpanIDHex == "" {
				return nil
			}
			cur = byID[cur.ParentSpanIDHex]
		}
		return nil
	}

	var out []SessionContextSpan
	for i := range spans {
		s := &spans[i]
		if s.TotalTokens == nil || *s.TotalTokens == 0 {
			continue
		}
		rootID, ok := rootByTrace[s.TraceIDHex]
		if !ok {
			continue
		}
		owner := ownerInvoke(s)
		if owner == nil || owner.SpanIDHex != rootID {
			continue
		}
		out = append(out, SessionContextSpan{
			TraceIDHex:          s.TraceIDHex,
			SpanIDHex:           s.SpanIDHex,
			Name:                s.Name,
			StartTimeMS:         s.StartTimeMS,
			InputTokens:         s.InputTokens,
			OutputTokens:        s.OutputTokens,
			TotalTokens:         s.TotalTokens,
			CacheReadTokens:     s.CacheReadTokens,
			CacheCreationTokens: s.CacheCreationTokens,
			GenAIRequestModel:   s.GenAIRequestModel,
		})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].StartTimeMS < out[j].StartTimeMS })
	return out
}
