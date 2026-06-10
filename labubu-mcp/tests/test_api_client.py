"""Tests for LabubuApiClient."""
import httpx
import pytest
from labubu_mcp.api_client import LabubuApiClient

pytestmark = pytest.mark.asyncio


class TestSearchTraces:
    async def test_returns_trace_list(self, api_client):
        result = await api_client.search_traces(status="ERROR")
        assert result["traces"] is not None
        assert len(result["traces"]) == 3
        assert result["pagination"]["total"] == 3

    async def test_passes_query_params(self, mock_http):
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
        def error_handler(request):
            raise httpx.ConnectError("Connection refused")
        transport = httpx.MockTransport(error_handler)
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await client.search_traces()
        assert "error" in result
        assert "Cannot connect" in result["error"]
        assert "labubu serve" in result["error"]
