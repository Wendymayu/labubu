-- Labubu trace storage schema for SQLite
-- These statements are executed on startup to ensure tables exist.
-- Translated from ClickHouse schema.sql for pure-Go SQLite store.

CREATE TABLE IF NOT EXISTS traces (
    trace_id_hex       TEXT NOT NULL PRIMARY KEY,
    root_span_id_hex   TEXT NOT NULL,
    root_name          TEXT NOT NULL,
    span_count         INTEGER NOT NULL DEFAULT 0,
    start_time_ms      INTEGER NOT NULL,
    end_time_ms        INTEGER NOT NULL,
    duration_ms        INTEGER NOT NULL,
    resource_attributes TEXT NOT NULL DEFAULT '{}',
    resource_schema_url TEXT NOT NULL DEFAULT '',
    scope_name          TEXT NOT NULL DEFAULT '',
    scope_version       TEXT NOT NULL DEFAULT '',
    scope_attributes    TEXT NOT NULL DEFAULT '{}',
    scope_schema_url    TEXT NOT NULL DEFAULT '',
    trace_state         TEXT NOT NULL DEFAULT '',
    dropped_span_count  INTEGER NOT NULL DEFAULT 0,
    status_code         TEXT NOT NULL DEFAULT 'UNSET',
    status_message      TEXT NOT NULL DEFAULT '',
    total_tokens        INTEGER,
    session_id          TEXT NOT NULL DEFAULT '',
    cost                REAL,
    cost_currency       TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_traces_start_time ON traces(start_time_ms);
CREATE INDEX IF NOT EXISTS idx_traces_session_id ON traces(session_id);

CREATE TABLE IF NOT EXISTS spans (
    trace_id_hex       TEXT NOT NULL,
    span_id_hex        TEXT NOT NULL,
    parent_span_id_hex TEXT NOT NULL DEFAULT '',
    trace_state        TEXT NOT NULL DEFAULT '',
    name               TEXT NOT NULL,
    kind               TEXT NOT NULL DEFAULT 'INTERNAL',
    start_time_ms      INTEGER NOT NULL,
    end_time_ms        INTEGER NOT NULL,
    duration_ms        INTEGER NOT NULL,
    attributes         TEXT NOT NULL DEFAULT '{}',
    dropped_attributes_count INTEGER NOT NULL DEFAULT 0,
    events             TEXT NOT NULL DEFAULT '[]',
    dropped_events_count INTEGER NOT NULL DEFAULT 0,
    links              TEXT NOT NULL DEFAULT '[]',
    dropped_links_count INTEGER NOT NULL DEFAULT 0,
    status_code        TEXT NOT NULL DEFAULT 'UNSET',
    status_message     TEXT NOT NULL DEFAULT '',
    input_tokens       INTEGER,
    output_tokens      INTEGER,
    total_tokens       INTEGER,
    cache_creation_tokens INTEGER,
    cache_read_tokens  INTEGER,
    gen_ai_request_model TEXT,
    cost               REAL,
    cost_currency      TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (trace_id_hex, span_id_hex)
);

CREATE INDEX IF NOT EXISTS idx_spans_trace_id ON spans(trace_id_hex);
CREATE INDEX IF NOT EXISTS idx_spans_start_time ON spans(trace_id_hex, start_time_ms);

CREATE TABLE IF NOT EXISTS logs (
    trace_id_hex  TEXT NOT NULL,
    span_id_hex   TEXT NOT NULL DEFAULT '',
    timestamp     INTEGER NOT NULL,
    severity      TEXT NOT NULL DEFAULT 'INFO',
    event_name    TEXT NOT NULL DEFAULT '',
    body          TEXT NOT NULL DEFAULT '',
    attributes    TEXT NOT NULL DEFAULT '{}',
    id            INTEGER PRIMARY KEY AUTOINCREMENT
);

CREATE INDEX IF NOT EXISTS idx_logs_trace_id ON logs(trace_id_hex);
CREATE INDEX IF NOT EXISTS idx_logs_timestamp ON logs(timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_severity ON logs(severity);
CREATE INDEX IF NOT EXISTS idx_logs_event_name ON logs(event_name);

CREATE TABLE IF NOT EXISTS model_pricing (
    model_name     TEXT NOT NULL PRIMARY KEY,
    input_price    REAL NOT NULL,
    output_price   REAL NOT NULL,
    currency       TEXT NOT NULL DEFAULT 'USD',
    context_window INTEGER NOT NULL DEFAULT 0,
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS llm_configs (
    id            TEXT NOT NULL PRIMARY KEY,
    model_name    TEXT NOT NULL,
    provider_type TEXT NOT NULL DEFAULT 'openai',
    provider_url  TEXT NOT NULL,
    api_key       TEXT NOT NULL DEFAULT '',
    is_default    INTEGER NOT NULL DEFAULT 0,
    temperature   REAL NOT NULL DEFAULT 0.7,
    max_tokens    INTEGER NOT NULL DEFAULT 4096,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
);

CREATE TABLE IF NOT EXISTS diagnosis_results (
    trace_id_hex  TEXT NOT NULL PRIMARY KEY,
    model_name    TEXT NOT NULL,
    scores        TEXT NOT NULL DEFAULT '{}',
    overall_score INTEGER NOT NULL DEFAULT 0,
    findings      TEXT NOT NULL DEFAULT '[]',
    summary       TEXT NOT NULL DEFAULT '',
    spans_snapshot TEXT NOT NULL DEFAULT '',
    raw_response  TEXT NOT NULL DEFAULT '',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    stale         INTEGER NOT NULL DEFAULT 0
);
