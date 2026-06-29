package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// expectedEndpoints is the canonical list of REST routes registered in router.go
// and its sub-handlers. Adding a new API requires adding a row here AND an entry
// in openAPISpecJSON — the two-way check below fails if they disagree.
var expectedEndpoints = []struct {
	method string
	path   string
}{
	{"get", "/api/v1/traces"},
	{"get", "/api/v1/traces/{id}"},
	{"get", "/api/v1/traces/{id}/diagnosis"},
	{"post", "/api/v1/traces/{id}/diagnose"},
	{"post", "/api/v1/traces/export"},
	{"post", "/api/v1/traces/import"},
	{"get", "/api/v1/services"},
	{"get", "/api/v1/sessions"},
	{"get", "/api/v1/sessions/{id}"},
	{"get", "/api/v1/sessions/{id}/agent-stats"},
	{"get", "/api/v1/logs"},
	{"get", "/api/v1/logs/{traceId}"},
	{"get", "/api/v1/log-event-names"},
	{"get", "/api/v1/query"},
	{"get", "/api/v1/query_range"},
	{"get", "/api/v1/labels"},
	{"get", "/api/v1/label/{name}/values"},
	{"get", "/api/v1/metadata"},
	{"get", "/api/v1/metric-names"},
	{"post", "/api/v1/otlp/v1/metrics"},
	{"get", "/api/v1/dashboards"},
	{"post", "/api/v1/dashboards"},
	{"put", "/api/v1/dashboards/{id}"},
	{"delete", "/api/v1/dashboards/{id}"},
	{"post", "/api/v1/dashboards/{id}/panels"},
	{"put", "/api/v1/dashboards/{id}/panels/{panelId}"},
	{"delete", "/api/v1/dashboards/{id}/panels/{panelId}"},
	{"get", "/api/v1/model-pricing"},
	{"post", "/api/v1/model-pricing"},
	{"post", "/api/v1/model-pricing/recalc"},
	{"delete", "/api/v1/model-pricing/{name}"},
	{"get", "/api/v1/llm-configs"},
	{"post", "/api/v1/llm-configs"},
	{"put", "/api/v1/llm-configs/{id}"},
	{"delete", "/api/v1/llm-configs/{id}"},
	{"get", "/api/v1/alerts/rules"},
	{"post", "/api/v1/alerts/rules"},
	{"get", "/api/v1/alerts/rules/{id}"},
	{"put", "/api/v1/alerts/rules/{id}"},
	{"delete", "/api/v1/alerts/rules/{id}"},
	{"get", "/api/v1/alerts/states"},
	{"get", "/api/v1/alerts/notifications"},
	{"get", "/api/v1/cost-summary"},
	{"get", "/api/health"},
}

func TestOpenAPIHandler_ServesValidSpec(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPIHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var spec map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if spec["openapi"] != "3.0.3" {
		t.Fatalf("openapi = %v, want 3.0.3", spec["openapi"])
	}
	if _, ok := spec["info"].(map[string]interface{}); !ok {
		t.Fatalf("missing info object")
	}
	paths, ok := spec["paths"].(map[string]interface{})
	if !ok {
		t.Fatalf("missing paths object")
	}

	// Two-way drift check: every expected endpoint must be in the spec,
	// and every spec path+method must be in the expected list.
	specMethods := map[string]map[string]bool{}
	for p, v := range paths {
		item, ok := v.(map[string]interface{})
		if !ok {
			t.Fatalf("path %q is not an object", p)
		}
		specMethods[p] = map[string]bool{}
		for m := range item {
			switch m {
			case "get", "post", "put", "delete", "patch":
				specMethods[p][m] = true
			}
		}
	}

	expected := map[string]map[string]bool{}
	for _, e := range expectedEndpoints {
		if expected[e.path] == nil {
			expected[e.path] = map[string]bool{}
		}
		expected[e.path][e.method] = true
	}

	for p, methods := range expected {
		got, ok := specMethods[p]
		if !ok {
			t.Errorf("spec missing path %q", p)
			continue
		}
		for m := range methods {
			if !got[m] {
				t.Errorf("spec missing %s %s", m, p)
			}
		}
	}
	for p, methods := range specMethods {
		for m := range methods {
			if !expected[p][m] {
				t.Errorf("spec has unexpected %s %s (not in expected route table)", m, p)
			}
		}
	}
}

func TestOpenAPIHandler_RejectsNonGet(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		OpenAPIHandler(rec, httptest.NewRequest(method, "/api/v1/openapi.json", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s /api/v1/openapi.json: status = %d, want 405", method, rec.Code)
		}
	}
}

// TestOpenAPIHandler_EveryOperationTagged keeps Swagger UI's module grouping
// complete and typo-free: every operation must carry exactly one tag, every
// tag must be declared in the top-level tags array, and no declared tag may be
// unused. Add this check when introducing or renaming module tags.
func TestOpenAPIHandler_EveryOperationTagged(t *testing.T) {
	rec := httptest.NewRecorder()
	OpenAPIHandler(rec, httptest.NewRequest(http.MethodGet, "/api/v1/openapi.json", nil))

	var spec map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &spec); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	declared := map[string]bool{}
	if raw, ok := spec["tags"]; ok {
		arr, ok := raw.([]interface{})
		if !ok {
			t.Fatalf("top-level tags is not an array")
		}
		for _, e := range arr {
			obj, ok := e.(map[string]interface{})
			if !ok {
				t.Fatalf("tags entry is not an object: %v", e)
			}
			name, _ := obj["name"].(string)
			if name == "" {
				t.Fatalf("tags entry has no name: %v", obj)
			}
			declared[name] = true
		}
	}
	if len(declared) == 0 {
		t.Fatalf("no top-level tags declared")
	}

	used := map[string]bool{}
	paths, _ := spec["paths"].(map[string]interface{})
	for p, v := range paths {
		item, _ := v.(map[string]interface{})
		for m, op := range item {
			switch m {
			case "get", "post", "put", "delete", "patch":
			default:
				continue
			}
			operation, _ := op.(map[string]interface{})
			raw, ok := operation["tags"]
			if !ok {
				t.Errorf("%s %s: missing tags", m, p)
				continue
			}
			arr, ok := raw.([]interface{})
			if !ok || len(arr) == 0 {
				t.Errorf("%s %s: tags is missing or empty", m, p)
				continue
			}
			if len(arr) > 1 {
				t.Errorf("%s %s: expected exactly one tag, got %v", m, p, arr)
			}
			for _, tname := range arr {
				s, _ := tname.(string)
				if s == "" {
					t.Errorf("%s %s: empty tag name", m, p)
					continue
				}
				if !declared[s] {
					t.Errorf("%s %s: tag %q is not declared in top-level tags", m, p, s)
				}
				used[s] = true
			}
		}
	}

	for name := range declared {
		if !used[name] {
			t.Errorf("top-level tag %q is declared but no operation uses it", name)
		}
	}
}
