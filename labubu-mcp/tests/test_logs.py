"""Tests for search_logs MCP tool."""
import httpx
import pytest
from labubu_mcp.tools.logs import search_logs

pytestmark = pytest.mark.asyncio


class TestSearchLogsTool:
    async def test_returns_formatted_table(self, api_client):
        result = await search_logs(api_client, trace_id="a" * 32)
        assert "TimeoutError" in result
        assert "ERROR" in result
        assert "WARN" in result
        assert "retrying" in result

    async def test_empty_result(self, api_client):
        transport = httpx.MockTransport(lambda req: httpx.Response(
            200, json={"logs": [], "pagination": {"page": 1, "page_size": 20, "total": 0}}
        ))
        from labubu_mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await search_logs(client)
        assert "No logs found" in result

    async def test_invalid_severity_gives_helpful_error(self, api_client):
        result = await search_logs(api_client, severity="ERRROR")
        assert "ERRROR" in result
        assert "ERROR" in result
