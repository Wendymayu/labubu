"""Integration test for the MCP server (tool registration)."""
import asyncio
import sys

import pytest
from mcp import ClientSession, StdioServerParameters
from mcp.client.stdio import stdio_client

from labubu_mcp.server import create_server

pytestmark = pytest.mark.asyncio


class TestServerCreation:
    async def test_creates_server_with_all_tools(self, api_client):
        server = create_server(api_client)
        assert server is not None
        assert server.name == "labubu"

        from labubu_mcp.tools.traces import search_traces, get_trace_detail
        from labubu_mcp.tools.logs import search_logs
        from labubu_mcp.tools.metrics import query_metrics
        from labubu_mcp.tools.services import list_services

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


class TestStdioTransport:
    """End-to-end stdio handshake — the path `claude mcp` / Claude Code uses.

    Spawns the server as a subprocess (exactly how an MCP client launches it),
    runs initialize + tools/list, and asserts the 5 tools are discoverable over
    stdio. Guards against the `run_stdio_async()`-not-awaited regression, where
    main() returned without awaiting the coroutine and the process exited
    before the handshake completed (Claude Code reported "Failed to connect").
    """

    async def test_stdio_lists_all_tools(self):
        params = StdioServerParameters(
            command=sys.executable,
            args=["-m", "labubu_mcp", "--api-url", "http://localhost:8080"],
        )
        async with stdio_client(params) as (read, write):
            async with ClientSession(read, write) as session:
                await asyncio.wait_for(session.initialize(), timeout=20)
                result = await asyncio.wait_for(session.list_tools(), timeout=20)
                # mcp client may return a ListToolsResult or a (result, meta) tuple.
                tool_list = result.tools if hasattr(result, "tools") else result[0].tools
                names = {t.name for t in tool_list}
                assert names == {
                    "search_traces",
                    "get_trace_detail",
                    "search_logs",
                    "query_metrics",
                    "list_services",
                }
