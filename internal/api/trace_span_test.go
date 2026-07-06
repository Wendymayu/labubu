package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labubu/labubu/internal/storage"
)

func TestGetTraceStripsHeavyAttributes(t *testing.T) {
	bigValue := strings.Repeat("x", 5000) // > 2048 threshold
	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "a1b2c3d4e5f600000000000000000000",
			SpanCount:  1,
			Spans: []storage.SpanDetail{{
				SpanID: "0123456789abcdef",
				Name:   "chat",
				Attributes: map[string]string{
					"gen_ai.input.messages":  bigValue,
					"gen_ai.request.model":   "gpt-4",
					"gen_ai.usage.input_tokens": "150",
				},
			}},
		},
	}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000", nil)
	rec := httptest.NewRecorder()
	handler.GetTrace(rec, req, "a1b2c3d4e5f600000000000000000000")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Trace *storage.TraceDetail `json:"trace"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	attrs := resp.Trace.Spans[0].Attributes
	if _, ok := attrs["gen_ai.input.messages"]; ok {
		t.Errorf("heavy gen_ai.input.messages should be stripped from bulk response")
	}
	if attrs["gen_ai.request.model"] != "gpt-4" {
		t.Errorf("small attribute should be preserved, got %q", attrs["gen_ai.request.model"])
	}
	if attrs["gen_ai.usage.input_tokens"] != "150" {
		t.Errorf("small attribute should be preserved, got %q", attrs["gen_ai.usage.input_tokens"])
	}
}

func TestGetSpanReturnsFullAttributes(t *testing.T) {
	bigValue := strings.Repeat("x", 5000)
	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "a1b2c3d4e5f600000000000000000000",
			SpanCount:  2,
			Spans: []storage.SpanDetail{
				{SpanID: "0123456789abcdef", Name: "root", Attributes: map[string]string{"gen_ai.request.model": "gpt-4"}},
				{SpanID: "fedcba9876543210", Name: "chat", Attributes: map[string]string{"gen_ai.input.messages": bigValue}},
			},
		},
	}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/spans/fedcba9876543210", nil)
	rec := httptest.NewRecorder()
	handler.GetSpan(rec, req, "a1b2c3d4e5f600000000000000000000", "fedcba9876543210")

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Span *storage.SpanDetail `json:"span"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Span.SpanID != "fedcba9876543210" {
		t.Fatalf("got span_id %q, want fedcba9876543210", resp.Span.SpanID)
	}
	if resp.Span.Attributes["gen_ai.input.messages"] != bigValue {
		t.Errorf("GetSpan must return FULL attributes (no stripping), got truncated/absent value")
	}
}

func TestGetSpanNotFound(t *testing.T) {
	store := &handlerMockStore{
		detail: &storage.TraceDetail{
			TraceIDHex: "a1b2c3d4e5f600000000000000000000",
			Spans:      []storage.SpanDetail{{SpanID: "0123456789abcdef"}},
		},
	}
	handler := NewTraceHandler(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/traces/a1b2c3d4e5f600000000000000000000/spans/0000000000000000", nil)
	rec := httptest.NewRecorder()
	handler.GetSpan(rec, req, "a1b2c3d4e5f600000000000000000000", "0000000000000000")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing span, got %d", rec.Code)
	}
}
