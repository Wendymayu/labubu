"""Tests for list_services MCP tool."""
import httpx
import pytest
from labubu.mcp.tools.services import list_services

pytestmark = pytest.mark.asyncio


class TestListServicesTool:
    async def test_returns_bullet_list(self, api_client):
        result = await list_services(api_client)
        assert "api-gateway" in result
        assert "embed-svc" in result
        assert "4" in result

    async def test_empty_services(self, api_client):
        transport = httpx.MockTransport(lambda req: httpx.Response(
            200, json={"services": []}
        ))
        from labubu.mcp.api_client import LabubuApiClient
        client = LabubuApiClient("http://localhost:8080", transport=transport)
        result = await list_services(client)
        assert "0" in result
