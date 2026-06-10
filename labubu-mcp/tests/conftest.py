"""Shared fixtures and sample data for MCP tests."""
import re
import pytest
import httpx

# ── Sample data matching Labubu REST API response shapes ──

SAMPLE_TRACES = {
    "traces": [
        {
            "trace_id_hex": "a" * 32,
            "root_span_id": "b" * 16,
            "root_name": "POST /api/chat",
            "root_service": "api-gateway",
            "span_count": 5,
            "start_time_ms": 1700000000000,
            "duration_ms": 2340,
            "status": "ERROR",
            "status_message": "upstream timeout",
            "total_tokens": 1500,
        },
        {
            "trace_id_hex": "c" * 32,
            "root_span_id": "d" * 16,
            "root_name": "GET /api/health",
            "root_service": "api-gateway",
            "span_count": 2,
            "start_time_ms": 1700000001000,
            "duration_ms": 120,
            "status": "OK",
            "status_message": "",
            "total_tokens": 0,
        },
        {
            "trace_id_hex": "e" * 32,
            "root_span_id": "f" * 16,
            "root_name": "POST /api/chat",
            "root_service": "api-gateway",
            "span_count": 8,
            "start_time_ms": 1700000002000,
            "duration_ms": 5100,
            "status": "ERROR",
            "status_message": "rate limit exceeded",
            "total_tokens": 3200,
        },
    ],
    "pagination": {"page": 1, "page_size": 20, "total": 3},
}

SAMPLE_TRACE_DETAIL = {
    "trace": {
        "trace_id_hex": "a" * 32,
        "root_span_id": "root001",
        "span_count": 3,
        "start_time_ms": 1700000000000,
        "duration_ms": 2340,
        "resource_attributes": {"service.name": "api-gateway"},
        "resource_schema_url": "",
        "scope": {
            "name": "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp",
            "version": "v0.45.0",
            "attributes": {},
        },
        "cost": None,
        "cost_currency": "",
        "unpriced_spans": 0,
        "spans": [
            {
                "span_id": "root001",
                "parent_span_id": "",
                "name": "POST /api/chat",
                "kind": "SERVER",
                "start_time_ms": 1700000000000,
                "duration_ms": 2340,
                "attributes": {"http.method": "POST", "http.url": "/api/chat"},
                "events": [
                    {
                        "name": "gen_ai.system_message",
                        "time_ms": 1700000000100,
                        "attributes": {"content": "You are a helpful assistant"},
                    },
                    {
                        "name": "gen_ai.error",
                        "time_ms": 1700000002300,
                        "attributes": {
                            "error.type": "TimeoutError",
                            "error.message": "request timeout after 30s",
                        },
                    },
                ],
                "links": [],
                "status": "ERROR",
                "status_message": "upstream timeout",
                "input_tokens": 500,
                "output_tokens": 0,
                "total_tokens": 500,
                "gen_ai_request_model": "gpt-4",
            },
            {
                "span_id": "child001",
                "parent_span_id": "root001",
                "name": "call_llm",
                "kind": "CLIENT",
                "start_time_ms": 1700000000050,
                "duration_ms": 2300,
                "attributes": {"gen_ai.request.model": "gpt-4"},
                "events": [],
                "links": [],
                "status": "ERROR",
                "status_message": "request timeout after 30s",
                "input_tokens": 500,
                "output_tokens": 0,
                "total_tokens": 500,
                "gen_ai_request_model": "gpt-4",
            },
            {
                "span_id": "child002",
                "parent_span_id": "root001",
                "name": "validate_input",
                "kind": "INTERNAL",
                "start_time_ms": 1700000000020,
                "duration_ms": 15,
                "attributes": {},
                "events": [],
                "links": [],
                "status": "OK",
                "status_message": "",
                "input_tokens": None,
                "output_tokens": None,
                "total_tokens": None,
                "gen_ai_request_model": None,
            },
        ],
    }
}

SAMPLE_LOGS = {
    "logs": [
        {
            "trace_id_hex": "a" * 32,
            "span_id_hex": "child001",
            "timestamp": 1700000002300,
            "severity": "ERROR",
            "event_name": "exception",
            "body": "TimeoutError: request timeout after 30s",
            "attributes": {"service.name": "api-gateway"},
        },
        {
            "trace_id_hex": "a" * 32,
            "span_id_hex": "child001",
            "timestamp": 1700000001000,
            "severity": "WARN",
            "event_name": "retry",
            "body": "retrying request (attempt 2/3)",
            "attributes": {},
        },
    ],
    "pagination": {"page": 1, "page_size": 20, "total": 2},
}

SAMPLE_SERVICES = {"services": ["api-gateway", "embed-svc", "llm-proxy", "auth-service"]}

SAMPLE_METRICS_RESPONSE = {
    "status": "success",
    "data": {
        "resultType": "vector",
        "result": [
            {"metric": {"service": "api-gateway"}, "value": [1700000000, "42.5"]},
            {"metric": {"service": "embed-svc"}, "value": [1700000000, "18.3"]},
            {"metric": {"service": "llm-proxy"}, "value": [1700000000, "7.1"]},
        ],
    },
}

SAMPLE_METRICS_ERROR = {
    "status": "error",
    "error": 'parse error at line 1, col 5: unexpected "}"',
}

# Route patterns for dynamic matching
TRACE_DETAIL_PATTERN = re.compile(r"^/api/v1/traces/([a-f0-9]{32})$")


def _mock_handler(request: httpx.Request) -> httpx.Response:
    """Route mock requests to appropriate responses based on URL path."""
    url_path = request.url.path
    query_str = request.url.query.decode() if request.url.query else ""

    # GET /api/v1/traces/{trace_id}
    m = TRACE_DETAIL_PATTERN.match(url_path)
    if m:
        trace_id = m.group(1)
        if trace_id == "ffffffffffffffffffffffffffffffff":
            return httpx.Response(404, json={"error": "trace not found"})
        return httpx.Response(200, json=SAMPLE_TRACE_DETAIL)

    # GET /api/v1/traces
    if url_path == "/api/v1/traces":
        return httpx.Response(200, json=SAMPLE_TRACES)

    # GET /api/v1/logs
    if url_path == "/api/v1/logs":
        return httpx.Response(200, json=SAMPLE_LOGS)

    # GET /api/v1/services
    if url_path == "/api/v1/services":
        return httpx.Response(200, json=SAMPLE_SERVICES)

    # GET /api/v1/query?query=...
    if url_path == "/api/v1/query":
        if "query=BAD" in query_str:
            return httpx.Response(200, json=SAMPLE_METRICS_ERROR)
        if "query=empty" in query_str:
            return httpx.Response(200, json={"status": "success", "data": {"resultType": "vector", "result": []}})
        return httpx.Response(200, json=SAMPLE_METRICS_RESPONSE)

    # Fallback
    return httpx.Response(200, json={})


# ── Fixtures ──

@pytest.fixture
def mock_http():
    """Return an httpx.MockTransport with a routing handler."""
    return httpx.MockTransport(_mock_handler)


@pytest.fixture
def api_client(mock_http):
    """Return a LabubuApiClient backed by mock HTTP transport."""
    from labubu_mcp.api_client import LabubuApiClient

    return LabubuApiClient("http://localhost:8080", transport=mock_http)
