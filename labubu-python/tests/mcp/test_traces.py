"""Tests for trace MCP tools."""
import httpx
import pytest
from labubu.mcp.tools.traces import search_traces, get_trace_detail

pytestmark = pytest.mark.asyncio


class TestSearchTracesTool:
    async def test_returns_formatted_table(self, api_client):
        result = await search_traces(api_client, status="ERROR")
        assert "POST /api/chat" in result
        assert "api-gateway" in result
        assert "ERROR" in result

    async def test_empty_result(self, api_client):
        transport = httpx.MockTransport(lambda req: httpx.Response(
            200, json={"traces": [], "pagination": {"page": 1, "page_size": 20, "total": 0}}
        ))
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await search_traces(client, status="OK")
        assert "No traces found" in result

    async def test_invalid_status_gives_helpful_error(self, api_client):
        result = await search_traces(api_client, status="ERRROR")
        assert "ERRROR" in result
        assert "ERROR" in result


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
