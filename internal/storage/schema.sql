-- Labubu trace storage schema for chDB (embedded ClickHouse)
-- These statements are executed on startup to ensure tables exist.

CREATE TABLE IF NOT EXISTS traces (
    trace_id               FixedString(16),
    trace_id_hex           String,
    root_span_id           FixedString(8),
    root_name              String,
    span_count             UInt16,
    start_time_ms          UInt64,
    end_time_ms            UInt64,
    duration_ms            UInt64,
    resource_attributes    Map(String, String),
    resource_schema_url    String,
    scope_name             String,
    scope_version          String,
    scope_attributes       Map(String, String),
    scope_schema_url       String,
    trace_state            String,
    dropped_span_count     UInt32,
    status_code            Enum8('UNSET'=0, 'OK'=1, 'ERROR'=2),
    status_message         String,
    total_tokens           Nullable(UInt32),
    session_id             String DEFAULT ''
)
ENGINE = MergeTree
ORDER BY (start_time_ms);

CREATE TABLE IF NOT EXISTS spans (
    trace_id               FixedString(16),
    span_id                FixedString(8),
    parent_span_id         FixedString(8),
    trace_state            String,
    name                   String,
    kind                   Enum8('UNSPECIFIED'=0, 'INTERNAL'=1, 'SERVER'=2, 'CLIENT'=3, 'PRODUCER'=4, 'CONSUMER'=5),
    start_time_ms          UInt64,
    end_time_ms            UInt64,
    duration_ms            UInt64,
    attributes             Map(String, String),
    dropped_attributes_count UInt32,
    events                 String,
    dropped_events_count   UInt32,
    links                  String,
    dropped_links_count    UInt32,
    status_code            Enum8('UNSET'=0, 'OK'=1, 'ERROR'=2),
    status_message         String,
    input_tokens           Nullable(UInt32),
    output_tokens          Nullable(UInt32),
    total_tokens           Nullable(UInt32),
    gen_ai_request_model   Nullable(String)
)
ENGINE = MergeTree
ORDER BY (trace_id, start_time_ms);

CREATE TABLE IF NOT EXISTS logs (
    trace_id    FixedString(16),
    span_id     FixedString(8),
    timestamp   UInt64,
    severity    String,
    event_name  String,
    body        String,
    attributes  Map(String, String)
)
ENGINE = MergeTree
ORDER BY (trace_id, timestamp);
