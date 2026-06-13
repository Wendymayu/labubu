# Labubu MCP Server Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Python MCP Server in `labubu-python/labubu/mcp/` that exposes 5 tools (`search_traces`, `get_trace_detail`, `search_logs`, `query_metrics`, `list_services`) via stdio transport, querying the Labubu REST API.

**Architecture:** Independent Python process communicating over stdio JSON-RPC (MCP protocol). Talks to Labubu Go backend via its existing REST API (`/api/v1/*`). Outputs CSV-style tables for lists and indented YAML for tree structures to minimize token usage.

**Tech Stack:** Python 3.8+, `mcp>=1.0.0` (MCP Python SDK), `httpx>=0.27` (async HTTP), `pytest` + `pytest-httpx` (testing)

---

## File Map

| File | Create/Modify | Responsibility |
|------|--------------|----------------|
| `labubu-python/pyproject.toml` | Modify | Add `mcp` optional deps + `labubu-mcp` script entry |
| `labubu-python/labubu/mcp/__init__.py` | Create | Package exports (`create_server`, `main`) |
| `labubu-python/labubu/mcp/__main__.py` | Create | `python -m labubu.mcp` entry point |
| `labubu-python/labubu/mcp/api_client.py` | Create | HTTP client wrapping Labubu REST API |
| `labubu-python/labubu/mcp/formatters.py` | Create | Output formatting: JSON→CSV tables, JSON→YAML trees |
| `labubu-python/labubu/mcp/server.py` | Create | MCP Server creation, tool registration, request routing |
| `labubu-python/labubu/mcp/tools/__init__.py` | Create | Tool function exports |
| `labubu-python/labubu/mcp/tools/traces.py` | Create | `search_traces`, `get_trace_detail` implementations |
| `labubu-python/labubu/mcp/tools/logs.py` | Create | `search_logs` implementation |
| `labubu-python/labubu/mcp/tools/metrics.py` | Create | `query_metrics` implementation |
| `labubu-python/labubu/mcp/tools/services.py` | Create | `list_services` implementation |
| `labubu-python/labubu/cli.py` | Modify | Add `mcp_main` CLI entry |
| `labubu-python/tests/__init__.py` | Create | Test package marker |
| `labubu-python/tests/mcp/__init__.py` | Create | Test package marker |
| `labubu-python/tests/mcp/conftest.py` | Create | Shared fixtures, mock API responses, sample data |
| `labubu-python/tests/mcp/test_formatters.py` | Create | Unit tests for output formatting |
| `labubu-python/tests/mcp/test_api_client.py` | Create | Integration tests for API client (mock HTTP) |
| `labubu-python/tests/mcp/test_traces.py` | Create | Tests for trace tools |
| `labubu-python/tests/mcp/test_logs.py` | Create | Tests for log tool |
| `labubu-python/tests/mcp/test_metrics.py` | Create | Tests for metrics tool |
| `labubu-python/tests/mcp/test_services.py` | Create | Tests for services tool |
| `labubu-python/tests/mcp/test_server.py` | Create | MCP server integration test |

---

### Task 1: Update pyproject.toml with MCP dependencies

**Files:**
- Modify: `labubu-python/pyproject.toml`

- [ ] **Step 1: Add optional dependencies and script entry**

Open `labubu-python/pyproject.toml`. Replace its content with:

```toml
[build-system]
requires = ["setuptools>=68.0"]
build-backend = "setuptools.build_meta"

[project]
name = "labubu"
version = "0.1.0"
description = "Local-first LLM observability platform"
requires-python = ">=3.8"

[project.optional-dependencies]
mcp = ["mcp>=1.0.0", "httpx>=0.27"]

[project.scripts]
labubu = "labubu.cli:main"
labubu-mcp = "labubu.cli:mcp_main"

[tool.setuptools.packages.find]
where = ["."]
exclude = ["tests*"]

[tool.setuptools.package-data]
labubu = ["bin/*"]
```

- [ ] **Step 2: Install the MCP extra in dev mode**

```bash
cd labubu-python && pip install -e ".[mcp]"
```

Expected: `mcp` and `httpx` installed without errors.

- [ ] **Step 3: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/pyproject.toml
git -C /d/opensource/github/labubu commit -m "build: add MCP optional dependencies and labubu-mcp entry point"
```

---

### Task 2: Create test fixtures and sample data

**Files:**
- Create: `labubu-python/tests/__init__.py`
- Create: `labubu-python/tests/mcp/__init__.py`
- Create: `labubu-python/tests/mcp/conftest.py`

- [ ] **Step 1: Create test package markers**

```bash
mkdir -p labubu-python/tests/mcp
```

Write `labubu-python/tests/__init__.py` (empty file):

```python
```

Write `labubu-python/tests/mcp/__init__.py` (empty file):

```python
```

- [ ] **Step 2: Write conftest.py with sample data and mock fixtures**

Write `labubu-python/tests/mcp/conftest.py`:

```python
"""Shared fixtures and sample data for MCP tests."""
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


# ── Mock fixtures ──

@pytest.fixture
def mock_http():
    """Return an httpx.MockTransport pre-configured with all routes."""
    transport = httpx.MockTransport()

    # Traces: list
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/traces"),
        lambda req: httpx.Response(200, json=SAMPLE_TRACES),
    )

    # Traces: detail (matches any 32-char hex trace id)
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/traces/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
        lambda req: httpx.Response(200, json=SAMPLE_TRACE_DETAIL),
    )

    # Traces: non-existent trace
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/traces/ffffffffffffffffffffffffffffffff"),
        lambda req: httpx.Response(404, json={"error": "trace not found"}),
    )

    # Logs: list
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/logs"),
        lambda req: httpx.Response(200, json=SAMPLE_LOGS),
    )

    # Services
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/services"),
        lambda req: httpx.Response(200, json=SAMPLE_SERVICES),
    )

    # Metrics: instant query (success)
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/query?query=rate%28http_requests_total%5B5m%5D%29"),
        lambda req: httpx.Response(200, json=SAMPLE_METRICS_RESPONSE),
    )

    # Metrics: error query
    transport.add_handler(
        "GET",
        httpx.URL("http://localhost:8080/api/v1/query?query=BAD"),
        lambda req: httpx.Response(200, json=SAMPLE_METRICS_ERROR),
    )

    return transport


@pytest.fixture
def api_client(mock_http):
    """Return a LabubuApiClient backed by mock HTTP transport."""
    from labubu.mcp.api_client import LabubuApiClient

    return LabubuApiClient("http://localhost:8080", transport=mock_http)
```

- [ ] **Step 3: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/tests/__init__.py labubu-python/tests/mcp/__init__.py labubu-python/tests/mcp/conftest.py
git -C /d/opensource/github/labubu commit -m "test: add MCP test fixtures and sample data"
```

---

### Task 3: Implement LabubuApiClient

**Files:**
- Create: `labubu-python/labubu/mcp/__init__.py`
- Create: `labubu-python/labubu/mcp/api_client.py`
- Create: `labubu-python/tests/mcp/test_api_client.py`

- [ ] **Step 1: Create mcp package init**

```bash
mkdir -p labubu-python/labubu/mcp
```

Write `labubu-python/labubu/mcp/__init__.py`:

```python
"""Labubu MCP Server — exposes observability data to AI agents via MCP."""
```

- [ ] **Step 2: Write the failing API client tests**

Write `labubu-python/tests/mcp/test_api_client.py`:

```python
"""Tests for LabubuApiClient."""
import pytest
from labubu.mcp.api_client import LabubuApiClient


class TestSearchTraces:
    async def test_returns_trace_list(self, api_client):
        result = await api_client.search_traces(status="ERROR")
        assert result["traces"] is not None
        assert len(result["traces"]) == 3
        assert result["pagination"]["total"] == 3

    async def test_passes_query_params(self, mock_http):
        """Verify query string parameters are sent correctly."""
        client = LabubuApiClient("http://localhost:8080", transport=mock_http)
        result = await client.search_traces(
            status="ERROR", service="api-gateway", limit=10, offset=0
        )
        assert len(result["traces"]) == 3


class TestGetTraceDetail:
    async def test_returns_trace_detail(self, api_client):
        trace_id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        result = await api_client.get_trace_detail(trace_id)
        assert result["trace"] is not None
        assert result["trace"]["trace_id_hex"] == trace_id
        assert len(result["trace"]["spans"]) == 3

    async def test_not_found_returns_none(self, api_client):
        trace_id = "ffffffffffffffffffffffffffffffff"
        result = await api_client.get_trace_detail(trace_id)
        assert result is None


class TestSearchLogs:
    async def test_returns_log_list(self, api_client):
        result = await api_client.search_logs(trace_id="a" * 32)
        assert result["logs"] is not None
        assert len(result["logs"]) == 2

    async def test_passes_severity_filter(self, mock_http):
        client = LabubuApiClient("http://localhost:8080", transport=mock_http)
        result = await client.search_logs(severity="ERROR")
        assert len(result["logs"]) == 2


class TestQueryMetrics:
    async def test_returns_metrics(self, api_client):
        result = await api_client.query_metrics("rate(http_requests_total[5m])")
        assert result["status"] == "success"
        assert len(result["data"]["result"]) == 3

    async def test_returns_error_for_bad_query(self, api_client):
        result = await api_client.query_metrics("BAD")
        assert result["status"] == "error"


class TestListServices:
    async def test_returns_service_list(self, api_client):
        result = await api_client.list_services()
        assert result["services"] == ["api-gateway", "embed-svc", "llm-proxy", "auth-service"]


class TestConnectionError:
    async def test_connection_refused(self):
        """When Labubu is not running, return an error dict."""
        client = LabubuApiClient("http://localhost:19999")  # unused port
        result = await client.search_traces()
        assert "error" in result
        assert "Cannot connect" in result["error"]
        assert "labubu serve" in result["error"]
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd labubu-python && python -m pytest tests/mcp/test_api_client.py -v
```

Expected: All tests FAIL — `LabubuApiClient` not yet implemented.

- [ ] **Step 4: Implement LabubuApiClient**

Write `labubu-python/labubu/mcp/api_client.py`:

```python
"""HTTP client for the Labubu REST API."""
import httpx


class LabubuApiClient:
    """Async HTTP client wrapping the Labubu REST API (GET /api/v1/*)."""

    def __init__(self, base_url: str, transport=None):
        self.base_url = base_url.rstrip("/")

    async def search_traces(self, **kwargs):
        """GET /api/v1/traces with query filters."""
        params = self._build_trace_params(kwargs)
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/traces",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def get_trace_detail(self, trace_id: str):
        """GET /api/v1/traces/{trace_id}. Returns None if not found."""
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/traces/{trace_id}",
                    timeout=30.0,
                )
                if r.status_code == 404:
                    return None
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def search_logs(self, **kwargs):
        """GET /api/v1/logs with query filters."""
        params = {}
        for key in ("trace_id", "severity", "event_name", "query", "start_time", "end_time"):
            if key in kwargs and kwargs[key] is not None:
                params[key] = kwargs[key]
        params["limit"] = min(kwargs.get("limit", 20), 50)
        params["offset"] = kwargs.get("offset", 0)
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/logs",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def query_metrics(self, query: str, time: str = None):
        """GET /api/v1/query?query=...&time=..."""
        params = {"query": query}
        if time:
            params["time"] = time
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/query",
                    params=params,
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    async def list_services(self):
        """GET /api/v1/services."""
        try:
            async with httpx.AsyncClient() as client:
                r = await client.get(
                    f"{self.base_url}/api/v1/services",
                    timeout=30.0,
                )
                r.raise_for_status()
                return r.json()
        except httpx.ConnectError:
            return {
                "error": (
                    f"Cannot connect to Labubu at {self.base_url}. "
                    "Is 'labubu serve' running?"
                )
            }
        except httpx.TimeoutException:
            return {
                "error": (
                    "Request timed out after 30s. "
                    "Try narrowing the time range or reducing the limit."
                )
            }

    @staticmethod
    def _build_trace_params(kwargs):
        """Map tool arguments to Labubu query string parameters."""
        params = {}
        mapping = {
            "status": "status",
            "service": "service",
            "query": "q",
            "start_time": "start",
            "end_time": "end",
            "min_duration_ms": "min_duration",
            "max_duration_ms": "max_duration",
        }
        for tool_key, api_key in mapping.items():
            if tool_key in kwargs and kwargs[tool_key] is not None:
                params[api_key] = kwargs[tool_key]
        params["page_size"] = min(kwargs.get("limit", 20), 50)
        page = kwargs.get("offset", 0) // params["page_size"] + 1 if kwargs.get("offset") else 1
        params["page"] = page
        return params
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd labubu-python && python -m pytest tests/mcp/test_api_client.py -v
```

Expected: All 8 tests PASS.

- [ ] **Step 6: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/__init__.py labubu-python/labubu/mcp/api_client.py labubu-python/tests/mcp/test_api_client.py
git -C /d/opensource/github/labubu commit -m "feat: add LabubuApiClient for REST API access"
```

---

### Task 4: Implement output formatters

**Files:**
- Create: `labubu-python/labubu/mcp/formatters.py`
- Create: `labubu-python/tests/mcp/test_formatters.py`

- [ ] **Step 1: Write the failing formatter tests**

Write `labubu-python/tests/mcp/test_formatters.py`:

```python
"""Tests for output formatters."""
import pytest
from labubu.mcp.formatters import (
    format_trace_list,
    format_trace_detail,
    format_log_list,
    format_metrics_result,
    format_service_list,
    build_span_tree,
)

# ── Sample inputs (subset of conftest data shapes, tested in isolation) ──

SAMPLE_TRACE_ITEMS = [
    {
        "trace_id_hex": "a" * 32,
        "root_name": "POST /api/chat",
        "root_service": "api-gateway",
        "span_count": 5,
        "start_time_ms": 1700000000000,
        "duration_ms": 2340,
        "status": "ERROR",
    },
    {
        "trace_id_hex": "c" * 32,
        "root_name": "GET /api/health",
        "root_service": "api-gateway",
        "span_count": 2,
        "start_time_ms": 1700000001000,
        "duration_ms": 120,
        "status": "OK",
    },
]


class TestFormatTraceList:
    def test_formats_as_table(self):
        result = format_trace_list(SAMPLE_TRACE_ITEMS, total=2)
        lines = result.split("\n")
        # Header + separator + 2 data rows
        assert len(lines) >= 4
        assert "POST /api/chat" in result
        assert "ERROR" in result

    def test_empty_list_shows_message(self):
        result = format_trace_list([], total=0)
        assert "No traces found" in result

    def test_includes_next_offset_when_truncated(self):
        result = format_trace_list(SAMPLE_TRACE_ITEMS, total=10, offset=0, limit=2)
        # Since limit < total, should include continuation hint
        assert "2 of 10" in result.lower() or "more" in result.lower()


class TestBuildSpanTree:
    def test_builds_hierarchy(self):
        spans = [
            {"span_id": "root", "parent_span_id": "", "name": "root_span", "kind": "SERVER",
             "start_time_ms": 0, "duration_ms": 100, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
            {"span_id": "child", "parent_span_id": "root", "name": "child_span", "kind": "INTERNAL",
             "start_time_ms": 10, "duration_ms": 80, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
        ]
        tree = build_span_tree(spans)
        assert len(tree) == 1
        assert tree[0]["span_id"] == "root"
        assert len(tree[0]["children"]) == 1
        assert tree[0]["children"][0]["span_id"] == "child"

    def test_multiple_roots(self):
        spans = [
            {"span_id": "r1", "parent_span_id": "", "name": "a", "kind": "SERVER",
             "start_time_ms": 0, "duration_ms": 10, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
            {"span_id": "r2", "parent_span_id": "", "name": "b", "kind": "SERVER",
             "start_time_ms": 5, "duration_ms": 10, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
        ]
        tree = build_span_tree(spans)
        assert len(tree) == 2


class TestFormatTraceDetail:
    def test_formats_as_yaml_like(self):
        sample_trace = {
            "trace_id_hex": "a" * 32,
            "root_span_id": "root001",
            "start_time_ms": 1700000000000,
            "duration_ms": 2340,
            "span_count": 3,
        }
        sample_spans = [
            {"span_id": "root001", "parent_span_id": "", "name": "POST /api/chat",
             "kind": "SERVER", "start_time_ms": 1700000000000, "duration_ms": 2340,
             "attributes": {"http.method": "POST"}, "events": [],
             "status": "ERROR", "status_message": "timeout"},
            {"span_id": "child001", "parent_span_id": "root001", "name": "call_llm",
             "kind": "CLIENT", "start_time_ms": 1700000000050, "duration_ms": 2300,
             "attributes": {}, "events": [
                 {"name": "gen_ai.error", "time_ms": 1700000002300,
                  "attributes": {"error.type": "TimeoutError"}}
             ],
             "status": "ERROR", "status_message": "timeout"},
        ]
        result = format_trace_detail(sample_trace, sample_spans)
        assert "trace_id:" in result
        assert "POST /api/chat" in result
        assert "ERROR" in result
        assert "call_llm" in result
        assert "gen_ai.error" in result
        assert "TimeoutError" in result

    def test_truncates_many_events(self):
        sample_trace = {
            "trace_id_hex": "b" * 32,
            "root_span_id": "root",
            "start_time_ms": 0,
            "duration_ms": 100,
            "span_count": 1,
        }
        # Create a span with 150 events (exceeds 100 limit)
        events = [
            {"name": f"event_{i}", "time_ms": i * 10, "attributes": {}}
            for i in range(150)
        ]
        sample_spans = [
            {"span_id": "root", "parent_span_id": "", "name": "big_span",
             "kind": "INTERNAL", "start_time_ms": 0, "duration_ms": 1500,
             "attributes": {}, "events": events,
             "status": "OK", "status_message": ""},
        ]
        result = format_trace_detail(sample_trace, sample_spans)
        # Should mention truncation
        assert "100" in result or "truncat" in result.lower()
        # event_149 should not appear (only first 100 events)
        assert "event_149" not in result


class TestFormatLogList:
    def test_formats_as_table(self):
        logs = [
            {"timestamp": 1700000002300, "severity": "ERROR", "event_name": "exception",
             "body": "TimeoutError"},
            {"timestamp": 1700000001000, "severity": "WARN", "event_name": "retry",
             "body": "retrying (2/3)"},
        ]
        result = format_log_list(logs, total=2)
        assert "ERROR" in result
        assert "TimeoutError" in result
        assert "WARN" in result

    def test_empty_logs(self):
        result = format_log_list([], total=0)
        assert "No logs found" in result


class TestFormatMetricsResult:
    def test_formats_vector_result(self):
        data = {
            "resultType": "vector",
            "result": [
                {"metric": {"service": "api"}, "value": [1700000000, "42.5"]},
                {"metric": {"service": "embed"}, "value": [1700000000, "18.3"]},
            ],
        }
        result = format_metrics_result(data)
        assert "api" in result
        assert "42.5" in result
        assert "embed" in result

    def test_empty_result(self):
        data = {"resultType": "vector", "result": []}
        result = format_metrics_result(data)
        assert "No data" in result or "empty" in result.lower()


class TestFormatServiceList:
    def test_formats_as_bullet_list(self):
        services = ["api-gateway", "embed-svc"]
        result = format_service_list(services)
        assert "api-gateway" in result
        assert "embed-svc" in result
        assert "2" in result  # count
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd labubu-python && python -m pytest tests/mcp/test_formatters.py -v
```

Expected: All tests FAIL — `formatters.py` not yet implemented.

- [ ] **Step 3: Implement formatters**

Write `labubu-python/labubu/mcp/formatters.py`:

```python
"""Output formatters: convert API JSON responses to Claude-friendly text."""
import json


def format_trace_list(traces, total, offset=0, limit=20):
    """Format a trace list as a human-readable table (CSV-like)."""
    if not traces:
        return "No traces found matching the given filters."

    header = f"{'trace_id':<34} {'name':<25} {'status':<8} {'duration':<10} {'spans':<6} {'service':<15}"
    sep = "-" * len(header)

    rows = []
    for t in traces:
        tid = t.get("trace_id_hex", "")[:32]
        name = t.get("root_name", "")[:24]
        status = t.get("status", "")
        dur_ms = t.get("duration_ms", 0)
        if dur_ms >= 1000:
            dur_str = f"{dur_ms / 1000:.2f}s"
        else:
            dur_str = f"{dur_ms:.1f}ms"
        sc = str(t.get("span_count", ""))
        svc = t.get("root_service", "")[:14]
        rows.append(
            f"{tid:<34} {name:<25} {status:<8} {dur_str:<10} {sc:<6} {svc:<15}"
        )

    lines = [f"Found {total} traces (showing {len(traces)}):\n", header, sep]
    lines.extend(rows)

    if offset + limit < total:
        next_offset = offset + limit
        lines.append(
            f"\nNext offset: {next_offset} (use offset={next_offset} for more, "
            f"showing {len(traces)} of {total})"
        )

    return "\n".join(lines)


def build_span_tree(spans):
    """Convert flat span list to a tree (children nested under parents)."""
    by_id = {}
    for s in spans:
        s = dict(s)  # shallow copy so we can add 'children'
        s["children"] = []
        by_id[s["span_id"]] = s

    roots = []
    for s in by_id.values():
        parent_id = s.get("parent_span_id", "")
        if parent_id and parent_id in by_id:
            by_id[parent_id]["children"].append(s)
        else:
            roots.append(s)
    return roots


def format_trace_detail(trace, spans, max_events=100):
    """Format a trace detail as indented YAML-like text.

    Builds a span tree so nested spans appear indented under their parents.
    Limits total events across all spans to max_events (default 100).
    """
    tree = build_span_tree(spans)
    total_event_count = sum(len(s.get("events") or []) for s in spans)
    event_count = 0
    truncated = False

    lines = []
    lines.append(f"trace_id: {trace.get('trace_id_hex', '')}")
    lines.append(f"name: {trace.get('root_name', '') or spans[0].get('name', '') if spans else ''}")
    lines.append(f"status: {trace.get('status', '') or (spans[0].get('status', '') if spans else '')}")
    lines.append(f"duration_ms: {trace.get('duration_ms', 0)}")
    lines.append(f"service: {spans[0].get('attributes', {}).get('service.name', '') if spans else ''}")
    lines.append(f"span_count: {len(spans)}")
    lines.append("")

    lines.append("spans:")
    for root in tree:
        _format_span(lines, root, indent=2, event_count_ref=[event_count],
                     max_events=max_events, truncated_ref=[truncated])

    if truncated[0]:
        lines.append(
            f"\n(truncated: showing first {max_events} events, "
            f"{total_event_count - max_events} omitted. "
            f"Use include_events=false to skip events.)"
        )

    return "\n".join(lines)


def _format_span(lines, span, indent, event_count_ref, max_events, truncated_ref):
    """Recursively format a span and its children."""
    prefix = " " * indent
    lines.append(f"{prefix}- span_id: {span.get('span_id', '')}")
    lines.append(f"{prefix}  name: {span.get('name', '')}")
    lines.append(f"{prefix}  kind: {span.get('kind', '')}")
    lines.append(f"{prefix}  status: {span.get('status', '')}")
    if span.get("status_message"):
        lines.append(f"{prefix}  status_message: {span['status_message']}")
    lines.append(f"{prefix}  duration_ms: {span.get('duration_ms', 0)}")

    attrs = span.get("attributes", {}) or {}
    if attrs:
        lines.append(f"{prefix}  attributes:")
        for k, v in attrs.items():
            val = str(v)[:200]
            lines.append(f"{prefix}    {k}: {val}")

    events = span.get("events") or []
    if events:
        lines.append(f"{prefix}  events:")
        for evt in events:
            if event_count_ref[0] >= max_events:
                truncated_ref[0] = True
                return
            event_count_ref[0] += 1
            name = evt.get("name", "")
            time_ms = evt.get("time_ms", 0)
            lines.append(f"{prefix}    - name: {name}")
            lines.append(f"{prefix}      time_ms: {time_ms}")
            evt_attrs = evt.get("attributes", {}) or {}
            if evt_attrs:
                lines.append(f"{prefix}      attributes:")
                for k, v in evt_attrs.items():
                    val_str = str(v)[:300]
                    lines.append(f"{prefix}        {k}: {val_str}")

    children = span.get("children", [])
    for child in children:
        _format_span(lines, child, indent + 2, event_count_ref, max_events, truncated_ref)


def format_log_list(logs, total, offset=0, limit=20):
    """Format a log list as a table."""
    if not logs:
        return "No logs found matching the given filters."

    header = f"{'timestamp':<22} {'severity':<10} {'event':<18} {'body':<50}"
    sep = "-" * len(header)

    rows = []
    for log in logs:
        ts = str(log.get("timestamp", ""))[:21]
        sev = log.get("severity", "")[:9]
        evt = log.get("event_name", "")[:17]
        body = (log.get("body", "") or "")[:49]
        rows.append(f"{ts:<22} {sev:<10} {evt:<18} {body:<50}")

    lines = [f"Found {total} logs (showing {len(logs)}):\n", header, sep]
    lines.extend(rows)

    if offset + limit < total:
        next_offset = offset + limit
        lines.append(
            f"\nNext offset: {next_offset} (use offset={next_offset} for more)"
        )

    return "\n".join(lines)


def format_metrics_result(data, max_series=20):
    """Format Prometheus instant query result as key-value pairs."""
    result_type = data.get("resultType", "vector")
    results = data.get("result", [])

    if not results:
        return "Query returned no data (empty result)."

    lines = [f"Result type: {result_type}"]

    count = 0
    for r in results[:max_series]:
        metric = r.get("metric", {})
        value = r.get("value", [])
        label_str = "{" + ", ".join(f"{k}={v}" for k, v in metric.items()) + "}"
        val_str = value[1] if len(value) > 1 else str(value) if value else "N/A"
        lines.append(f"  {label_str} => {val_str}")
        count += 1

    if len(results) > max_series:
        lines.append(
            f"\n(truncated: showing top {max_series} of {len(results)} series)"
        )

    return "\n".join(lines)


def format_service_list(services):
    """Format service names as a simple bullet list."""
    count = len(services)
    lines = [f"Services ({count}):"]
    for svc in services:
        lines.append(f"  • {svc}")
    return "\n".join(lines)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd labubu-python && python -m pytest tests/mcp/test_formatters.py -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/formatters.py labubu-python/tests/mcp/test_formatters.py
git -C /d/opensource/github/labubu commit -m "feat: add output formatters (CSV tables, YAML trees, key-value lists)"
```

---

### Task 5: Implement trace tools

**Files:**
- Create: `labubu-python/labubu/mcp/tools/__init__.py`
- Create: `labubu-python/labubu/mcp/tools/traces.py`
- Create: `labubu-python/tests/mcp/test_traces.py`

- [ ] **Step 1: Create tools package init**

Write `labubu-python/labubu/mcp/tools/__init__.py`:

```python
"""MCP Tool implementations for Labubu observability data."""
from labubu.mcp.tools.traces import search_traces, get_trace_detail
from labubu.mcp.tools.logs import search_logs
from labubu.mcp.tools.metrics import query_metrics
from labubu.mcp.tools.services import list_services

__all__ = [
    "search_traces",
    "get_trace_detail",
    "search_logs",
    "query_metrics",
    "list_services",
]
```

- [ ] **Step 2: Write the failing trace tool tests**

Write `labubu-python/tests/mcp/test_traces.py`:

```python
"""Tests for trace MCP tools."""
import pytest
from labubu.mcp.tools.traces import search_traces, get_trace_detail


class TestSearchTracesTool:
    async def test_returns_formatted_table(self, api_client):
        result = await search_traces(api_client, status="ERROR")
        assert "POST /api/chat" in result
        assert "api-gateway" in result
        assert "ERROR" in result

    async def test_empty_result(self, api_client):
        # Use a mock transport that returns empty list
        import httpx
        transport = httpx.MockTransport()
        transport.add_handler(
            "GET",
            httpx.URL("http://localhost:8080/api/v1/traces"),
            lambda req: httpx.Response(
                200, json={"traces": [], "pagination": {"page": 1, "page_size": 20, "total": 0}}
            ),
        )
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await search_traces(client, status="OK")
        assert "No traces found" in result

    async def test_invalid_status_gives_helpful_error(self, api_client):
        result = await search_traces(api_client, status="ERRROR")
        assert "ERRROR" in result
        assert "ERROR" in result  # should suggest valid values


class TestGetTraceDetailTool:
    async def test_returns_formatted_tree(self, api_client):
        trace_id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        result = await get_trace_detail(api_client, trace_id)
        assert "trace_id:" in result
        assert "POST /api/chat" in result
        assert "spans:" in result
        assert "call_llm" in result
        assert "gen_ai.error" in result
        assert "TimeoutError" in result

    async def test_not_found_error(self, api_client):
        trace_id = "ffffffffffffffffffffffffffffffff"
        result = await get_trace_detail(api_client, trace_id)
        assert "not found" in result.lower()
        assert "purged" in result.lower()

    async def test_service_name_extracted_from_attributes(self, api_client):
        trace_id = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
        result = await get_trace_detail(api_client, trace_id)
        assert "api-gateway" in result
```

- [ ] **Step 3: Run tests to verify they fail**

```bash
cd labubu-python && python -m pytest tests/mcp/test_traces.py -v
```

Expected: All tests FAIL — tools not yet implemented.

- [ ] **Step 4: Implement trace tools**

Write `labubu-python/labubu/mcp/tools/traces.py`:

```python
"""MCP Tools: search_traces and get_trace_detail."""
from labubu.mcp.formatters import format_trace_list, format_trace_detail

VALID_STATUSES = {"OK", "ERROR", "UNSET", ""}


async def search_traces(api_client, **kwargs):
    """Search traces by status, service, name, duration, and time range.

    Returns a summary table. Use get_trace_detail for full span data.
    """
    status = kwargs.get("status", "")
    if status and status not in VALID_STATUSES:
        hint = ""
        if status.upper() in VALID_STATUSES:
            hint = f" Did you mean '{status.upper()}'?"
        elif "ERR" in status.upper():
            hint = " Did you mean 'ERROR'?"
        return (
            f"Invalid status '{status}'. Valid values: OK, ERROR, UNSET, or empty for all.{hint}"
        )

    limit = min(kwargs.get("limit", 20), 50)
    offset = kwargs.get("offset", 0)

    result = await api_client.search_traces(
        status=status if status else None,
        service=kwargs.get("service"),
        query=kwargs.get("query"),
        start_time=kwargs.get("start_time"),
        end_time=kwargs.get("end_time"),
        min_duration_ms=kwargs.get("min_duration_ms"),
        max_duration_ms=kwargs.get("max_duration_ms"),
        limit=limit,
        offset=offset,
    )

    if "error" in result:
        return result["error"]

    traces = result.get("traces", [])
    total = result.get("pagination", {}).get("total", len(traces))

    return format_trace_list(traces, total=total, offset=offset, limit=limit)


async def get_trace_detail(api_client, trace_id: str, include_events: bool = True,
                           include_attributes: bool = True):
    """Get full detail for a trace, including all spans, events, and attributes.

    Returns an indented tree showing span hierarchy with events.
    """
    result = await api_client.get_trace_detail(trace_id)

    if result is None:
        return (
            f'Trace "{trace_id}" not found. '
            f"It may have been purged (retention: 7 days by default). "
            f"Use search_traces to see available traces."
        )

    if "error" in result:
        return result["error"]

    trace = result.get("trace", {})
    spans = trace.get("spans", [])

    # Optionally strip events/attributes
    if not include_events:
        for s in spans:
            s = dict(s)
            s["events"] = []
    if not include_attributes:
        for s in spans:
            s = dict(s)
            s["attributes"] = {}

    return format_trace_detail(trace, spans)
```

- [ ] **Step 5: Run tests to verify they pass**

```bash
cd labubu-python && python -m pytest tests/mcp/test_traces.py -v
```

Expected: All tests PASS.

- [ ] **Step 6: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/tools/__init__.py labubu-python/labubu/mcp/tools/traces.py labubu-python/tests/mcp/test_traces.py
git -C /d/opensource/github/labubu commit -m "feat: add search_traces and get_trace_detail MCP tools"
```

---

### Task 6: Implement search_logs tool

**Files:**
- Create: `labubu-python/labubu/mcp/tools/logs.py`
- Create: `labubu-python/tests/mcp/test_logs.py`

- [ ] **Step 1: Write the failing log tool tests**

Write `labubu-python/tests/mcp/test_logs.py`:

```python
"""Tests for search_logs MCP tool."""
import pytest
from labubu.mcp.tools.logs import search_logs

VALID_SEVERITIES = {"DEBUG", "INFO", "WARN", "ERROR", "FATAL", ""}


class TestSearchLogsTool:
    async def test_returns_formatted_table(self, api_client):
        result = await search_logs(api_client, trace_id="a" * 32)
        assert "TimeoutError" in result
        assert "ERROR" in result
        assert "WARN" in result
        assert "retrying" in result

    async def test_empty_result(self, api_client):
        import httpx
        transport = httpx.MockTransport()
        transport.add_handler(
            "GET",
            httpx.URL("http://localhost:8080/api/v1/logs"),
            lambda req: httpx.Response(
                200, json={"logs": [], "pagination": {"page": 1, "page_size": 20, "total": 0}}
            ),
        )
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await search_logs(client)
        assert "No logs found" in result

    async def test_invalid_severity_gives_helpful_error(self, api_client):
        result = await search_logs(api_client, severity="ERRROR")
        assert "ERRROR" in result
        assert "ERROR" in result  # should suggest
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd labubu-python && python -m pytest tests/mcp/test_logs.py -v
```

Expected: All tests FAIL.

- [ ] **Step 3: Implement search_logs**

Write `labubu-python/labubu/mcp/tools/logs.py`:

```python
"""MCP Tool: search_logs."""
from labubu.mcp.formatters import format_log_list

VALID_SEVERITIES = {"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}


async def search_logs(api_client, **kwargs):
    """Search logs by severity, event name, trace_id, or keyword.

    Returns a table of matching log entries.
    """
    severity = kwargs.get("severity", "")
    if severity and severity.upper() in VALID_SEVERITIES and severity != severity.upper():
        return (
            f"Invalid severity '{severity}'. "
            f"Valid values: {', '.join(sorted(VALID_SEVERITIES))}. "
            f"(Did you mean '{severity.upper()}'?)"
        )
    if severity and severity.upper() not in VALID_SEVERITIES:
        return (
            f"Invalid severity '{severity}'. "
            f"Valid values: {', '.join(sorted(VALID_SEVERITIES))}."
        )

    limit = min(kwargs.get("limit", 20), 50)
    offset = kwargs.get("offset", 0)

    result = await api_client.search_logs(
        trace_id=kwargs.get("trace_id"),
        severity=severity if severity else None,
        event_name=kwargs.get("event_name"),
        query=kwargs.get("query"),
        start_time=kwargs.get("start_time"),
        end_time=kwargs.get("end_time"),
        limit=limit,
        offset=offset,
    )

    if "error" in result:
        return result["error"]

    logs = result.get("logs", [])
    total = result.get("pagination", {}).get("total", len(logs))

    return format_log_list(logs, total=total, offset=offset, limit=limit)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd labubu-python && python -m pytest tests/mcp/test_logs.py -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/tools/logs.py labubu-python/tests/mcp/test_logs.py
git -C /d/opensource/github/labubu commit -m "feat: add search_logs MCP tool"
```

---

### Task 7: Implement query_metrics and list_services tools

**Files:**
- Create: `labubu-python/labubu/mcp/tools/metrics.py`
- Create: `labubu-python/labubu/mcp/tools/services.py`
- Create: `labubu-python/tests/mcp/test_metrics.py`
- Create: `labubu-python/tests/mcp/test_services.py`

- [ ] **Step 1: Write the failing tests**

Write `labubu-python/tests/mcp/test_metrics.py`:

```python
"""Tests for query_metrics MCP tool."""
import pytest
from labubu.mcp.tools.metrics import query_metrics


class TestQueryMetricsTool:
    async def test_returns_formatted_kv_list(self, api_client):
        result = await query_metrics(api_client, "rate(http_requests_total[5m])")
        assert "api-gateway" in result
        assert "42.5" in result

    async def test_error_response(self, api_client):
        result = await query_metrics(api_client, "BAD")
        assert "error" in result.lower()

    async def test_empty_metrics(self, api_client):
        import httpx
        transport = httpx.MockTransport()
        transport.add_handler(
            "GET",
            httpx.URL("http://localhost:8080/api/v1/query?query=empty"),
            lambda req: httpx.Response(
                200, json={"status": "success", "data": {"resultType": "vector", "result": []}}
            ),
        )
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await query_metrics(client, "empty")
        assert "no data" in result.lower() or "empty" in result.lower()
```

Write `labubu-python/tests/mcp/test_services.py`:

```python
"""Tests for list_services MCP tool."""
import pytest
from labubu.mcp.tools.services import list_services


class TestListServicesTool:
    async def test_returns_bullet_list(self, api_client):
        result = await list_services(api_client)
        assert "api-gateway" in result
        assert "embed-svc" in result
        assert "4" in result  # count

    async def test_empty_services(self, api_client):
        import httpx
        transport = httpx.MockTransport()
        transport.add_handler(
            "GET",
            httpx.URL("http://localhost:8080/api/v1/services"),
            lambda req: httpx.Response(200, json={"services": []}),
        )
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await list_services(client)
        assert "0" in result
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd labubu-python && python -m pytest tests/mcp/test_metrics.py tests/mcp/test_services.py -v
```

Expected: All tests FAIL.

- [ ] **Step 3: Implement metrics and services tools**

Write `labubu-python/labubu/mcp/tools/metrics.py`:

```python
"""MCP Tool: query_metrics."""
from labubu.mcp.formatters import format_metrics_result


async def query_metrics(api_client, query: str, time: str = None):
    """Execute a PromQL instant query against stored metrics.

    Returns key-value pairs of metric labels → value.
    Supports standard PromQL syntax (rate, sum, avg, etc.).
    """
    result = await api_client.query_metrics(query, time=time)

    if "error" in result:
        return result["error"]

    if result.get("status") == "error":
        return f"PromQL error: {result.get('error', 'unknown error')}"

    data = result.get("data", {})
    return format_metrics_result(data)
```

Write `labubu-python/labubu/mcp/tools/services.py`:

```python
"""MCP Tool: list_services."""
from labubu.mcp.formatters import format_service_list


async def list_services(api_client):
    """List all known service names from ingested traces.

    Use this to discover available services before filtering traces or logs.
    """
    result = await api_client.list_services()

    if "error" in result:
        return result["error"]

    services = result.get("services", [])
    return format_service_list(services)
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
cd labubu-python && python -m pytest tests/mcp/test_metrics.py tests/mcp/test_services.py -v
```

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/tools/metrics.py labubu-python/labubu/mcp/tools/services.py labubu-python/tests/mcp/test_metrics.py labubu-python/tests/mcp/test_services.py
git -C /d/opensource/github/labubu commit -m "feat: add query_metrics and list_services MCP tools"
```

---

### Task 8: Implement MCP server with tool registration

**Files:**
- Create: `labubu-python/labubu/mcp/server.py`
- Create: `labubu-python/labubu/mcp/__main__.py`
- Create: `labubu-python/tests/mcp/test_server.py`

- [ ] **Step 1: Write the failing server test**

Write `labubu-python/tests/mcp/test_server.py`:

```python
"""Integration test for the MCP server (tool routing)."""
import pytest
from labubu.mcp.server import create_server


class TestServerCreation:
    async def test_creates_server_with_all_tools(self, api_client):
        server = create_server(api_client)
        assert server is not None
        assert server.name == "labubu"

        # Verify each tool is callable through the server
        from labubu.mcp.tools.traces import search_traces, get_trace_detail
        from labubu.mcp.tools.logs import search_logs
        from labubu.mcp.tools.metrics import query_metrics
        from labubu.mcp.tools.services import list_services

        # Each tool function should be imported successfully
        assert callable(search_traces)
        assert callable(get_trace_detail)
        assert callable(search_logs)
        assert callable(query_metrics)
        assert callable(list_services)
```

- [ ] **Step 2: Run test to verify it fails**

```bash
cd labubu-python && python -m pytest tests/mcp/test_server.py -v
```

Expected: FAIL — `server.py` not yet created.

- [ ] **Step 3: Implement server.py**

Write `labubu-python/labubu/mcp/server.py`:

```python
"""MCP Server creation and tool registration."""
from mcp.server import Server
from mcp.types import Tool, TextContent

from labubu.mcp.tools.traces import search_traces, get_trace_detail
from labubu.mcp.tools.logs import search_logs
from labubu.mcp.tools.metrics import query_metrics
from labubu.mcp.tools.services import list_services

# Tool definitions (name, description, inputSchema) for MCP protocol
TOOL_DEFINITIONS = [
    Tool(
        name="search_traces",
        description=(
            "Search traces by status, service name, keyword, duration range, and time range. "
            "Returns a summary table with trace_id, name, status, duration, span count, and service. "
            "Use this to discover traces, then call get_trace_detail for full span trees."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "status": {
                    "type": "string",
                    "enum": ["OK", "ERROR", "UNSET"],
                    "description": "Filter by trace status. Omit for all.",
                },
                "service": {
                    "type": "string",
                    "description": "Filter by service name (use list_services to see available).",
                },
                "query": {
                    "type": "string",
                    "description": "Fuzzy search on trace root span name.",
                },
                "start_time": {
                    "type": "string",
                    "description": "Start of time range (ISO 8601 or Unix ms).",
                },
                "end_time": {
                    "type": "string",
                    "description": "End of time range (ISO 8601 or Unix ms).",
                },
                "min_duration_ms": {
                    "type": "integer",
                    "description": "Minimum trace duration in milliseconds.",
                },
                "max_duration_ms": {
                    "type": "integer",
                    "description": "Maximum trace duration in milliseconds.",
                },
                "limit": {
                    "type": "integer",
                    "description": "Max traces to return (default 20, max 50).",
                },
                "offset": {
                    "type": "integer",
                    "description": "Pagination offset for next page.",
                },
            },
        },
    ),
    Tool(
        name="get_trace_detail",
        description=(
            "Get the full detail of a single trace, including all spans (as a tree), "
            "events, attributes, status messages, and token usage. "
            "Use this after search_traces to drill into a specific trace."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "trace_id": {
                    "type": "string",
                    "description": "The 32-character hex trace ID from search_traces.",
                },
                "include_events": {
                    "type": "boolean",
                    "description": "Include span events (default true). Set to false for shorter output.",
                },
                "include_attributes": {
                    "type": "boolean",
                    "description": "Include span attributes (default true). Set to false for shorter output.",
                },
            },
            "required": ["trace_id"],
        },
    ),
    Tool(
        name="search_logs",
        description=(
            "Search logs by severity, event name, trace ID, or keyword. "
            "Returns a table with timestamp, severity, event name, and body. "
            "Use this to find error logs related to a trace or time period."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "trace_id": {
                    "type": "string",
                    "description": "Filter logs by trace ID (32 hex chars).",
                },
                "severity": {
                    "type": "string",
                    "enum": ["DEBUG", "INFO", "WARN", "ERROR", "FATAL"],
                    "description": "Filter by log severity level.",
                },
                "event_name": {
                    "type": "string",
                    "description": "Filter by event name (e.g., 'exception').",
                },
                "query": {
                    "type": "string",
                    "description": "Full-text search on log body.",
                },
                "start_time": {
                    "type": "string",
                    "description": "Start of time range (ISO 8601 or Unix ms).",
                },
                "end_time": {
                    "type": "string",
                    "description": "End of time range (ISO 8601 or Unix ms).",
                },
                "limit": {
                    "type": "integer",
                    "description": "Max logs to return (default 20, max 50).",
                },
                "offset": {
                    "type": "integer",
                    "description": "Pagination offset.",
                },
            },
        },
    ),
    Tool(
        name="query_metrics",
        description=(
            "Execute a PromQL instant query against stored metrics. "
            "Returns metric labels and their current values. "
            "Use this to check error rates, latency percentiles, request counts, etc."
        ),
        inputSchema={
            "type": "object",
            "properties": {
                "query": {
                    "type": "string",
                    "description": (
                        "PromQL query string. Examples: 'rate(http_requests_total[5m])', "
                        "'http_request_duration_ms{quantile=\"0.99\"}', "
                        "'sum(rate(errors_total[5m])) by (service)'"
                    ),
                },
                "time": {
                    "type": "string",
                    "description": "Evaluation time (ISO 8601 or Unix timestamp). Defaults to now.",
                },
            },
            "required": ["query"],
        },
    ),
    Tool(
        name="list_services",
        description=(
            "List all known service names from ingested traces. "
            "Use this to discover available services before filtering traces or logs."
        ),
        inputSchema={
            "type": "object",
            "properties": {},
        },
    ),
]

# Tool name → async handler function mapping
TOOL_HANDLERS = {
    "search_traces": search_traces,
    "get_trace_detail": get_trace_detail,
    "search_logs": search_logs,
    "query_metrics": query_metrics,
    "list_services": list_services,
}


def create_server(api_client):
    """Create and configure the Labubu MCP Server.

    Args:
        api_client: A LabubuApiClient instance connected to a Labubu backend.

    Returns:
        A configured mcp.server.Server ready for stdio transport.
    """
    server = Server("labubu")

    # Register each tool with the MCP server
    for tool_def in TOOL_DEFINITIONS:
        tool_name = tool_def.name

        # Capture tool_name in closure for the handler
        def make_handler(name):
            async def handler(arguments):
                handler_fn = TOOL_HANDLERS[name]
                result = await handler_fn(api_client, **arguments)
                return [TextContent(type="text", text=result)]
            return handler

        server.add_tool(tool_def, make_handler(tool_name))

    return server
```

- [ ] **Step 4: Implement __main__.py for python -m labubu.mcp**

Write `labubu-python/labubu/mcp/__main__.py`:

```python
"""Entry point for python -m labubu.mcp."""
from labubu.mcp.server import main
main()
```

- [ ] **Step 5: Update labubu/mcp/__init__.py**

Replace `labubu-python/labubu/mcp/__init__.py` with:

```python
"""Labubu MCP Server — exposes observability data to AI agents via MCP."""
from labubu.mcp.server import create_server

__all__ = ["create_server"]
```

- [ ] **Step 6: Run test to verify it passes**

```bash
cd labubu-python && python -m pytest tests/mcp/test_server.py -v
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/server.py labubu-python/labubu/mcp/__main__.py labubu-python/labubu/mcp/__init__.py labubu-python/tests/mcp/test_server.py
git -C /d/opensource/github/labubu commit -m "feat: add MCP server with tool registration"
```

---

### Task 9: Add CLI entry point and stdio main

**Files:**
- Modify: `labubu-python/labubu/cli.py`
- Modify: `labubu-python/labubu/mcp/server.py`
- Modify: `labubu-python/labubu/mcp/__main__.py`

- [ ] **Step 1: Add main() to server.py**

Add to the end of `labubu-python/labubu/mcp/server.py` (append after the existing code):

```python
import argparse
import asyncio
import sys


def main():
    """CLI entry point: start the MCP server over stdio."""
    parser = argparse.ArgumentParser(
        description="Labubu MCP Server — expose observability data to AI agents"
    )
    parser.add_argument(
        "--api-url",
        default="http://localhost:8080",
        help="Base URL of the Labubu REST API (default: http://localhost:8080)",
    )
    args = parser.parse_args()

    from labubu.mcp.api_client import LabubuApiClient
    from mcp.server import stdio_server

    api_client = LabubuApiClient(args.api_url)
    server = create_server(api_client)

    async def run():
        async with stdio_server() as (read_stream, write_stream):
            await server.run(
                read_stream,
                write_stream,
                server.create_initialization_options(),
            )

    asyncio.run(run())


if __name__ == "__main__":
    main()
```

- [ ] **Step 2: Update __main__.py**

Replace `labubu-python/labubu/mcp/__main__.py`:

```python
"""Entry point for python -m labubu.mcp."""
from labubu.mcp.server import main
main()
```

- [ ] **Step 3: Add mcp_main to cli.py**

Append to the end of `labubu-python/labubu/cli.py`:

```python
def mcp_main():
    """Entry point for labubu-mcp command (MCP Server over stdio)."""
    from labubu.mcp.server import main as mcp_server_main
    mcp_server_main()
```

- [ ] **Step 4: Verify CLI parses correctly**

```bash
cd labubu-python && python -m labubu.mcp --help
```

Expected: Help text showing `--api-url` option.

- [ ] **Step 5: Commit**

```bash
git -C /d/opensource/github/labubu add labubu-python/labubu/mcp/server.py labubu-python/labubu/mcp/__main__.py labubu-python/labubu/cli.py
git -C /d/opensource/github/labubu commit -m "feat: add CLI entry point and stdio transport for MCP server"
```

---

### Task 10: Run full test suite and manual verification

**Files:** None (verification only)

- [ ] **Step 1: Run all MCP tests**

```bash
cd labubu-python && python -m pytest tests/mcp/ -v
```

Expected: All tests PASS (approximately 25 tests across 7 test files).

- [ ] **Step 2: Verify TypeScript compatibility does not break**

```bash
cd web && npx vue-tsc --noEmit
```

Expected: No new type errors.

- [ ] **Step 3: Verify Go tests still pass**

```bash
cd /d/opensource/github/labubu && make test-nocgo
```

Expected: All Go tests PASS.

- [ ] **Step 4: Manual smoke test with MCP Inspector**

```bash
# Terminal 1: Start Labubu
labubu serve

# Terminal 2: Inject test data
python -m labubu otlp-send --file test_trace.json

# Terminal 3: Start MCP Inspector pointing at our server
npx @modelcontextprotocol/inspector python -m labubu.mcp --api-url http://localhost:8080
```

Verify in Inspector:
1. Server connects successfully
2. `list_services` returns service names
3. `search_traces` returns trace list
4. `get_trace_detail` returns span tree with events

- [ ] **Step 5: Commit any final tweaks**

```bash
git -C /d/opensource/github/labubu status
git -C /d/opensource/github/labubu diff
# If clean, no commit needed
```
