package receiver

import (
	"testing"
)

func TestTranslateSpanBasic(t *testing.T) {
	// Basic smoke test: nil spans should produce empty result.
	spans := translateSpans(nil)
	if len(spans) != 0 {
		t.Errorf("expected 0 spans from nil input, got %d", len(spans))
	}
}

func TestNewReceiver(t *testing.T) {
	// Verify that New accepts nil store and does not panic.
	r := New(nil, nil, nil)
	if r == nil {
		t.Error("expected non-nil receiver")
	}
}

func TestAnyValueToString(t *testing.T) {
	if s := anyValueToString(nil); s != "" {
		t.Errorf("expected empty string for nil, got %q", s)
	}
}

func TestKeyValueToMap(t *testing.T) {
	if m := keyValueToMap(nil); len(m) != 0 {
		t.Errorf("expected empty map for nil, got %v", m)
	}
}
