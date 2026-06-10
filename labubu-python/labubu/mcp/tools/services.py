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
