"""MCP Tool: query_metrics."""
from labubu_mcp.formatters import format_metrics_result


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
