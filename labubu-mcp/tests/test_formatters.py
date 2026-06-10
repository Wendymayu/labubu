"""Tests for output formatters."""
import pytest
from labubu_mcp.formatters import (
    format_trace_list,
    format_trace_detail,
    format_log_list,
    format_metrics_result,
    format_service_list,
    build_span_tree,
)

SAMPLE_TRACE_ITEMS = [
    {
        "trace_id_hex": "a" * 32,
        "root_name": "POST /api/chat",
        "root_service": "api-gateway",
        "span_count": 5,
        "start_time_ms": 1700000000000,
        "duration_ms": 2340,
        "status": "ERROR",
    },
    {
        "trace_id_hex": "c" * 32,
        "root_name": "GET /api/health",
        "root_service": "api-gateway",
        "span_count": 2,
        "start_time_ms": 1700000001000,
        "duration_ms": 120,
        "status": "OK",
    },
]


class TestFormatTraceList:
    def test_formats_as_table(self):
        result = format_trace_list(SAMPLE_TRACE_ITEMS, total=2)
        lines = result.split("\n")
        assert len(lines) >= 4
        assert "POST /api/chat" in result
        assert "ERROR" in result

    def test_empty_list_shows_message(self):
        result = format_trace_list([], total=0)
        assert "No traces found" in result

    def test_includes_next_offset_when_truncated(self):
        result = format_trace_list(SAMPLE_TRACE_ITEMS, total=10, offset=0, limit=2)
        assert "2 of 10" in result.lower() or "more" in result.lower()


class TestBuildSpanTree:
    def test_builds_hierarchy(self):
        spans = [
            {"span_id": "root", "parent_span_id": "", "name": "root_span", "kind": "SERVER",
             "start_time_ms": 0, "duration_ms": 100, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
            {"span_id": "child", "parent_span_id": "root", "name": "child_span", "kind": "INTERNAL",
             "start_time_ms": 10, "duration_ms": 80, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
        ]
        tree = build_span_tree(spans)
        assert len(tree) == 1
        assert tree[0]["span_id"] == "root"
        assert len(tree[0]["children"]) == 1
        assert tree[0]["children"][0]["span_id"] == "child"

    def test_multiple_roots(self):
        spans = [
            {"span_id": "r1", "parent_span_id": "", "name": "a", "kind": "SERVER",
             "start_time_ms": 0, "duration_ms": 10, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
            {"span_id": "r2", "parent_span_id": "", "name": "b", "kind": "SERVER",
             "start_time_ms": 5, "duration_ms": 10, "attributes": {}, "events": [],
             "status": "OK", "status_message": ""},
        ]
        tree = build_span_tree(spans)
        assert len(tree) == 2


class TestFormatTraceDetail:
    def test_formats_as_yaml_like(self):
        sample_trace = {
            "trace_id_hex": "a" * 32,
            "root_span_id": "root001",
            "start_time_ms": 1700000000000,
            "duration_ms": 2340,
            "span_count": 3,
        }
        sample_spans = [
            {"span_id": "root001", "parent_span_id": "", "name": "POST /api/chat",
             "kind": "SERVER", "start_time_ms": 1700000000000, "duration_ms": 2340,
             "attributes": {"http.method": "POST"}, "events": [],
             "status": "ERROR", "status_message": "timeout"},
            {"span_id": "child001", "parent_span_id": "root001", "name": "call_llm",
             "kind": "CLIENT", "start_time_ms": 1700000000050, "duration_ms": 2300,
             "attributes": {}, "events": [
                 {"name": "gen_ai.error", "time_ms": 1700000002300,
                  "attributes": {"error.type": "TimeoutError"}}
             ],
             "status": "ERROR", "status_message": "timeout"},
        ]
        result = format_trace_detail(sample_trace, sample_spans)
        assert "trace_id:" in result
        assert "POST /api/chat" in result
        assert "ERROR" in result
        assert "call_llm" in result
        assert "gen_ai.error" in result
        assert "TimeoutError" in result

    def test_truncates_many_events(self):
        sample_trace = {
            "trace_id_hex": "b" * 32,
            "root_span_id": "root",
            "start_time_ms": 0,
            "duration_ms": 100,
            "span_count": 1,
        }
        events = [
            {"name": f"event_{i}", "time_ms": i * 10, "attributes": {}}
            for i in range(150)
        ]
        sample_spans = [
            {"span_id": "root", "parent_span_id": "", "name": "big_span",
             "kind": "INTERNAL", "start_time_ms": 0, "duration_ms": 1500,
             "attributes": {}, "events": events,
             "status": "OK", "status_message": ""},
        ]
        result = format_trace_detail(sample_trace, sample_spans)
        assert "100" in result or "truncat" in result.lower()
        assert "event_149" not in result


class TestFormatLogList:
    def test_formats_as_table(self):
        logs = [
            {"timestamp": 1700000002300, "severity": "ERROR", "event_name": "exception",
             "body": "TimeoutError"},
            {"timestamp": 1700000001000, "severity": "WARN", "event_name": "retry",
             "body": "retrying (2/3)"},
        ]
        result = format_log_list(logs, total=2)
        assert "ERROR" in result
        assert "TimeoutError" in result
        assert "WARN" in result

    def test_empty_logs(self):
        result = format_log_list([], total=0)
        assert "No logs found" in result


class TestFormatMetricsResult:
    def test_formats_vector_result(self):
        data = {
            "resultType": "vector",
            "result": [
                {"metric": {"service": "api"}, "value": [1700000000, "42.5"]},
                {"metric": {"service": "embed"}, "value": [1700000000, "18.3"]},
            ],
        }
        result = format_metrics_result(data)
        assert "api" in result
        assert "42.5" in result
        assert "embed" in result

    def test_empty_result(self):
        data = {"resultType": "vector", "result": []}
        result = format_metrics_result(data)
        assert "No data" in result or "empty" in result.lower()


class TestFormatServiceList:
    def test_formats_as_bullet_list(self):
        services = ["api-gateway", "embed-svc"]
        result = format_service_list(services)
        assert "api-gateway" in result
        assert "embed-svc" in result
        assert "2" in result
