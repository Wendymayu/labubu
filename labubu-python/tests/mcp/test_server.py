"""Integration test for the MCP server (tool registration)."""
import pytest
from labubu.mcp.server import create_server

pytestmark = pytest.mark.asyncio


class TestServerCreation:
    async def test_creates_server_with_all_tools(self, api_client):
        server = create_server(api_client)
        assert server is not None
        assert server.name == "labubu"

        # Verify each tool function is importable and callable
        from labubu.mcp.tools.traces import search_traces, get_trace_detail
        from labubu.mcp.tools.logs import search_logs
        from labubu.mcp.tools.metrics import query_metrics
        from labubu.mcp.tools.services import list_services

        assert callable(search_traces)
        assert callable(get_trace_detail)
        assert callable(search_logs)
        assert callable(query_metrics)
        assert callable(list_services)

    async def test_tools_registered(self, api_client):
        server = create_server(api_client)
        tools = await server.list_tools()
        tool_names = {t.name for t in tools}
        assert "search_traces" in tool_names
        assert "get_trace_detail" in tool_names
        assert "search_logs" in tool_names
        assert "query_metrics" in tool_names
        assert "list_services" in tool_names
        assert len(tool_names) == 5
