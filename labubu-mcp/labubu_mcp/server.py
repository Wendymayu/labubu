"""MCP Server creation and tool registration."""
import argparse

from mcp.server.fastmcp import FastMCP

from labubu_mcp.tools.traces import search_traces, get_trace_detail
from labubu_mcp.tools.logs import search_logs
from labubu_mcp.tools.metrics import query_metrics
from labubu_mcp.tools.services import list_services


def create_server(api_client):
    """Create and configure the Labubu MCP Server.

    Args:
        api_client: A LabubuApiClient instance connected to a Labubu backend.

    Returns:
        A configured FastMCP server ready for stdio transport.
    """
    server = FastMCP("labubu")

    @server.tool(name="search_traces")
    async def _search_traces(
        status: str = "",
        service: str = "",
        query: str = "",
        start_time: str = "",
        end_time: str = "",
        min_duration_ms: int = 0,
        max_duration_ms: int = 0,
        limit: int = 20,
        offset: int = 0,
    ) -> str:
        """Search traces by status, service name, keyword, duration range, and time range.

        Returns a summary table with trace_id, name, status, duration, span count, and service.
        Use this to discover traces, then call get_trace_detail for full span trees.

        Args:
            status: Filter by trace status (OK, ERROR, UNSET). Omit for all.
            service: Filter by service name (use list_services to see available).
            query: Fuzzy search on trace root span name.
            start_time: Start of time range (ISO 8601 or Unix ms).
            end_time: End of time range (ISO 8601 or Unix ms).
            min_duration_ms: Minimum trace duration in milliseconds.
            max_duration_ms: Maximum trace duration in milliseconds.
            limit: Max traces to return (default 20, max 50).
            offset: Pagination offset for next page.
        """
        kwargs = {}
        if status:
            kwargs["status"] = status
        if service:
            kwargs["service"] = service
        if query:
            kwargs["query"] = query
        if start_time:
            kwargs["start_time"] = start_time
        if end_time:
            kwargs["end_time"] = end_time
        if min_duration_ms:
            kwargs["min_duration_ms"] = min_duration_ms
        if max_duration_ms:
            kwargs["max_duration_ms"] = max_duration_ms
        kwargs["limit"] = limit
        kwargs["offset"] = offset
        return await search_traces(api_client, **kwargs)

    @server.tool(name="get_trace_detail")
    async def _get_trace_detail(
        trace_id: str,
        include_events: bool = True,
        include_attributes: bool = True,
    ) -> str:
        """Get the full detail of a single trace, including all spans (as a tree),
        events, attributes, status messages, and token usage.
        Use this after search_traces to drill into a specific trace.

        Args:
            trace_id: The 32-character hex trace ID from search_traces.
            include_events: Include span events (default true). Set to false for shorter output.
            include_attributes: Include span attributes (default true). Set to false for shorter output.
        """
        return await get_trace_detail(
            api_client, trace_id,
            include_events=include_events,
            include_attributes=include_attributes,
        )

    @server.tool(name="search_logs")
    async def _search_logs(
        trace_id: str = "",
        severity: str = "",
        event_name: str = "",
        query: str = "",
        start_time: str = "",
        end_time: str = "",
        limit: int = 20,
        offset: int = 0,
    ) -> str:
        """Search logs by severity, event name, trace ID, or keyword.
        Returns a table with timestamp, severity, event name, and body.
        Use this to find error logs related to a trace or time period.

        Args:
            trace_id: Filter logs by trace ID (32 hex chars).
            severity: Filter by log severity level (DEBUG, INFO, WARN, ERROR, FATAL).
            event_name: Filter by event name (e.g., 'exception').
            query: Full-text search on log body.
            start_time: Start of time range (ISO 8601 or Unix ms).
            end_time: End of time range (ISO 8601 or Unix ms).
            limit: Max logs to return (default 20, max 50).
            offset: Pagination offset.
        """
        kwargs = {}
        if trace_id:
            kwargs["trace_id"] = trace_id
        if severity:
            kwargs["severity"] = severity
        if event_name:
            kwargs["event_name"] = event_name
        if query:
            kwargs["query"] = query
        if start_time:
            kwargs["start_time"] = start_time
        if end_time:
            kwargs["end_time"] = end_time
        kwargs["limit"] = limit
        kwargs["offset"] = offset
        return await search_logs(api_client, **kwargs)

    @server.tool(name="query_metrics")
    async def _query_metrics(
        query: str,
        time: str = "",
    ) -> str:
        """Execute a PromQL instant query against stored metrics.
        Returns metric labels and their current values.
        Use this to check error rates, latency percentiles, request counts, etc.

        Args:
            query: PromQL query string. Examples: 'rate(http_requests_total[5m])',
                   'http_request_duration_ms{quantile="0.99"}',
                   'sum(rate(errors_total[5m])) by (service)'
            time: Evaluation time (ISO 8601 or Unix timestamp). Defaults to now.
        """
        return await query_metrics(api_client, query, time=time if time else None)

    @server.tool(name="list_services")
    async def _list_services() -> str:
        """List all known service names from ingested traces.
        Use this to discover available services before filtering traces or logs.
        """
        return await list_services(api_client)

    return server


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

    from labubu_mcp.api_client import LabubuApiClient

    api_client = LabubuApiClient(args.api_url)
    server = create_server(api_client)
    server.run_stdio_async()


if __name__ == "__main__":
    main()
