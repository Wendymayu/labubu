"""Tests for query_metrics MCP tool."""
import httpx
import pytest
from labubu_mcp.tools.metrics import query_metrics

pytestmark = pytest.mark.asyncio


class TestQueryMetricsTool:
    async def test_returns_formatted_kv_list(self, api_client):
        result = await query_metrics(api_client, "rate(http_requests_total[5m])")
        assert "api-gateway" in result
        assert "42.5" in result

    async def test_error_response(self, api_client):
        result = await query_metrics(api_client, "BAD")
        assert "error" in result.lower()

    async def test_empty_metrics(self, api_client):
        transport = httpx.MockTransport(lambda req: httpx.Response(
            200, json={"status": "success", "data": {"resultType": "vector", "result": []}}
        ))
        from labubu_mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await query_metrics(client, "empty")
        assert "no data" in result.lower() or "empty" in result.lower()
