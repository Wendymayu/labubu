"""MCP Tools: search_traces and get_trace_detail."""
from labubu.mcp.formatters import format_trace_list, format_trace_detail

VALID_STATUSES = {"OK", "ERROR", "UNSET", ""}


async def search_traces(api_client, **kwargs):
    """Search traces by status, service, name, duration, and time range.

    Returns a summary table. Use get_trace_detail for full span data.
    """
    status = kwargs.get("status", "")
    if status and status not in VALID_STATUSES:
        hint = ""
        if status.upper() in VALID_STATUSES:
            hint = f" Did you mean '{status.upper()}'?"
        elif "ERR" in status.upper():
            hint = " Did you mean 'ERROR'?"
        return (
            f"Invalid status '{status}'. Valid values: OK, ERROR, UNSET, or empty for all.{hint}"
        )

    limit = min(kwargs.get("limit", 20), 50)
    offset = kwargs.get("offset", 0)

    result = await api_client.search_traces(
        status=status if status else None,
        service=kwargs.get("service"),
        query=kwargs.get("query"),
        start_time=kwargs.get("start_time"),
        end_time=kwargs.get("end_time"),
        min_duration_ms=kwargs.get("min_duration_ms"),
        max_duration_ms=kwargs.get("max_duration_ms"),
        limit=limit,
        offset=offset,
    )

    if "error" in result:
        return result["error"]

    traces = result.get("traces", [])
    total = result.get("pagination", {}).get("total", len(traces))

    return format_trace_list(traces, total=total, offset=offset, limit=limit)


async def get_trace_detail(api_client, trace_id: str, include_events: bool = True,
                           include_attributes: bool = True):
    """Get full detail for a trace, including all spans, events, and attributes.

    Returns an indented tree showing span hierarchy with events.
    """
    result = await api_client.get_trace_detail(trace_id)

    if result is None:
        return (
            f'Trace "{trace_id}" not found. '
            f"It may have been purged (retention: 7 days by default). "
            f"Use search_traces to see available traces."
        )

    if "error" in result:
        return result["error"]

    trace = result.get("trace", {})
    spans = trace.get("spans", [])

    # Optionally strip events/attributes
    if not include_events:
        for s in spans:
            s = dict(s)
            s["events"] = []
    if not include_attributes:
        for s in spans:
            s = dict(s)
            s["attributes"] = {}

    return format_trace_detail(trace, spans)
