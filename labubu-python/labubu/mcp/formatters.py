"""Output formatters: convert API JSON responses to Claude-friendly text."""


def format_trace_list(traces, total, offset=0, limit=20):
    """Format a trace list as a human-readable table (CSV-like)."""
    if not traces:
        return "No traces found matching the given filters."

    header = f"{'trace_id':<34} {'name':<25} {'status':<8} {'duration':<10} {'spans':<6} {'service':<15}"
    sep = "-" * len(header)

    rows = []
    for t in traces:
        tid = t.get("trace_id_hex", "")[:32]
        name = t.get("root_name", "")[:24]
        status = t.get("status", "")
        dur_ms = t.get("duration_ms", 0)
        if dur_ms >= 1000:
            dur_str = f"{dur_ms / 1000:.2f}s"
        else:
            dur_str = f"{dur_ms:.1f}ms"
        sc = str(t.get("span_count", ""))
        svc = t.get("root_service", "")[:14]
        rows.append(
            f"{tid:<34} {name:<25} {status:<8} {dur_str:<10} {sc:<6} {svc:<15}"
        )

    lines = [f"Found {total} traces (showing {len(traces)}):\n", header, sep]
    lines.extend(rows)

    if offset + limit < total:
        next_offset = offset + limit
        lines.append(
            f"\nNext offset: {next_offset} (use offset={next_offset} for more, "
            f"showing {len(traces)} of {total})"
        )

    return "\n".join(lines)


def build_span_tree(spans):
    """Convert flat span list to a tree (children nested under parents)."""
    by_id = {}
    for s in spans:
        s = dict(s)  # shallow copy so we can add 'children'
        s["children"] = []
        by_id[s["span_id"]] = s

    roots = []
    for s in by_id.values():
        parent_id = s.get("parent_span_id", "")
        if parent_id and parent_id in by_id:
            by_id[parent_id]["children"].append(s)
        else:
            roots.append(s)
    return roots


def format_trace_detail(trace, spans, max_events=100):
    """Format a trace detail as indented YAML-like text.

    Builds a span tree so nested spans appear indented under their parents.
    Limits total events across all spans to max_events (default 100).
    """
    tree = build_span_tree(spans)
    total_event_count = sum(len(s.get("events") or []) for s in spans)
    event_count = 0
    truncated = False

    lines = []
    lines.append(f"trace_id: {trace.get('trace_id_hex', '')}")
    lines.append(f"name: {trace.get('root_name', '') or (spans[0].get('name', '') if spans else '')}")
    lines.append(f"status: {trace.get('status', '') or (spans[0].get('status', '') if spans else '')}")
    lines.append(f"duration_ms: {trace.get('duration_ms', 0)}")
    lines.append(f"service: {spans[0].get('attributes', {}).get('service.name', '') if spans else ''}")
    lines.append(f"span_count: {len(spans)}")
    lines.append("")

    lines.append("spans:")
    for root in tree:
        _format_span(lines, root, indent=2, event_count_ref=[event_count],
                     max_events=max_events, truncated_ref=[truncated])

    if truncated:
        lines.append(
            f"\n(truncated: showing first {max_events} events, "
            f"{total_event_count - max_events} omitted. "
            f"Use include_events=false to skip events.)"
        )

    return "\n".join(lines)


def _format_span(lines, span, indent, event_count_ref, max_events, truncated_ref):
    """Recursively format a span and its children."""
    prefix = " " * indent
    lines.append(f"{prefix}- span_id: {span.get('span_id', '')}")
    lines.append(f"{prefix}  name: {span.get('name', '')}")
    lines.append(f"{prefix}  kind: {span.get('kind', '')}")
    lines.append(f"{prefix}  status: {span.get('status', '')}")
    if span.get("status_message"):
        lines.append(f"{prefix}  status_message: {span['status_message']}")
    lines.append(f"{prefix}  duration_ms: {span.get('duration_ms', 0)}")

    attrs = span.get("attributes", {}) or {}
    if attrs:
        lines.append(f"{prefix}  attributes:")
        for k, v in attrs.items():
            val = str(v)[:200]
            lines.append(f"{prefix}    {k}: {val}")

    events = span.get("events") or []
    if events:
        lines.append(f"{prefix}  events:")
        for evt in events:
            if event_count_ref[0] >= max_events:
                truncated_ref[0] = True
                return
            event_count_ref[0] += 1
            name = evt.get("name", "")
            time_ms = evt.get("time_ms", 0)
            lines.append(f"{prefix}    - name: {name}")
            lines.append(f"{prefix}      time_ms: {time_ms}")
            evt_attrs = evt.get("attributes", {}) or {}
            if evt_attrs:
                lines.append(f"{prefix}      attributes:")
                for k, v in evt_attrs.items():
                    val_str = str(v)[:300]
                    lines.append(f"{prefix}        {k}: {val_str}")

    children = span.get("children", [])
    for child in children:
        _format_span(lines, child, indent + 2, event_count_ref, max_events, truncated_ref)


def format_log_list(logs, total, offset=0, limit=20):
    """Format a log list as a table."""
    if not logs:
        return "No logs found matching the given filters."

    header = f"{'timestamp':<22} {'severity':<10} {'event':<18} {'body':<50}"
    sep = "-" * len(header)

    rows = []
    for log in logs:
        ts = str(log.get("timestamp", ""))[:21]
        sev = log.get("severity", "")[:9]
        evt = log.get("event_name", "")[:17]
        body = (log.get("body", "") or "")[:49]
        rows.append(f"{ts:<22} {sev:<10} {evt:<18} {body:<50}")

    lines = [f"Found {total} logs (showing {len(logs)}):\n", header, sep]
    lines.extend(rows)

    if offset + limit < total:
        next_offset = offset + limit
        lines.append(
            f"\nNext offset: {next_offset} (use offset={next_offset} for more)"
        )

    return "\n".join(lines)


def format_metrics_result(data, max_series=20):
    """Format Prometheus instant query result as key-value pairs."""
    result_type = data.get("resultType", "vector")
    results = data.get("result", [])

    if not results:
        return "Query returned no data (empty result)."

    lines = [f"Result type: {result_type}"]

    for r in results[:max_series]:
        metric = r.get("metric", {})
        value = r.get("value", [])
        label_str = "{" + ", ".join(f"{k}={v}" for k, v in metric.items()) + "}"
        val_str = value[1] if len(value) > 1 else str(value) if value else "N/A"
        lines.append(f"  {label_str} => {val_str}")

    if len(results) > max_series:
        lines.append(
            f"\n(truncated: showing top {max_series} of {len(results)} series)"
        )

    return "\n".join(lines)


def format_service_list(services):
    """Format service names as a simple bullet list."""
    count = len(services)
    lines = [f"Services ({count}):"]
    for svc in services:
        lines.append(f"  • {svc}")
    return "\n".join(lines)
