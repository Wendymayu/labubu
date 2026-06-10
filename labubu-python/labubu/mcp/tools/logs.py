"""MCP Tool: search_logs."""
from labubu.mcp.formatters import format_log_list

VALID_SEVERITIES = {"DEBUG", "INFO", "WARN", "ERROR", "FATAL"}


async def search_logs(api_client, **kwargs):
    """Search logs by severity, event name, trace_id, or keyword.

    Returns a table of matching log entries.
    """
    severity = kwargs.get("severity", "")
    if severity and severity.upper() in VALID_SEVERITIES and severity != severity.upper():
        return (
            f"Invalid severity '{severity}'. "
            f"Valid values: {', '.join(sorted(VALID_SEVERITIES))}. "
            f"(Did you mean '{severity.upper()}'?)"
        )
    if severity and severity.upper() not in VALID_SEVERITIES:
        return (
            f"Invalid severity '{severity}'. "
            f"Valid values: {', '.join(sorted(VALID_SEVERITIES))}."
        )

    limit = min(kwargs.get("limit", 20), 50)
    offset = kwargs.get("offset", 0)

    result = await api_client.search_logs(
        trace_id=kwargs.get("trace_id"),
        severity=severity if severity else None,
        event_name=kwargs.get("event_name"),
        query=kwargs.get("query"),
        start_time=kwargs.get("start_time"),
        end_time=kwargs.get("end_time"),
        limit=limit,
        offset=offset,
    )

    if "error" in result:
        return result["error"]

    logs = result.get("logs", [])
    total = result.get("pagination", {}).get("total", len(logs))

    return format_log_list(logs, total=total, offset=offset, limit=limit)
