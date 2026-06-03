package receiver

import (
	"testing"

	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
)

func TestTranslateMetrics_Gauge(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "test-svc"}}},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test-scope", Version: "1.0"},
						Metrics: []*metricspb.Metric{
							{
								Name:        "gen_ai_client_token_usage",
								Description: "Token usage",
								Unit:        "tokens",
								Data: &metricspb.Metric_Gauge{
									Gauge: &metricspb.Gauge{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1717000000000000000,
												Attributes: []*commonpb.KeyValue{
													{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus"}}},
												},
												Value: &metricspb.NumberDataPoint_AsInt{AsInt: 4500},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}

	p := points[0]
	if p.Name != "gen_ai_client_token_usage" {
		t.Errorf("expected name 'gen_ai_client_token_usage', got %q", p.Name)
	}
	if p.Value != 4500.0 {
		t.Errorf("expected value 4500.0, got %f", p.Value)
	}
	if p.Labels["service"] != "test-svc" {
		t.Errorf("expected service label 'test-svc', got %q", p.Labels["service"])
	}
	if p.Labels["model"] != "claude-opus" {
		t.Errorf("expected model label 'claude-opus', got %q", p.Labels["model"])
	}
	if p.Labels["scope_name"] != "test-scope" {
		t.Errorf("expected scope_name label 'test-scope', got %q", p.Labels["scope_name"])
	}
	if p.Timestamp != 1717000000000 {
		t.Errorf("expected timestamp 1717000000000, got %d", p.Timestamp)
	}
}

func TestTranslateMetrics_Sum(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test"},
						Metrics: []*metricspb.Metric{
							{
								Name: "requests_total",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
										IsMonotonic:            true,
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1717000000000000000,
												Value:        &metricspb.NumberDataPoint_AsDouble{AsDouble: 99.5},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	if len(points) != 1 {
		t.Fatalf("expected 1 point, got %d", len(points))
	}
	if points[0].Value != 99.5 {
		t.Errorf("expected 99.5, got %f", points[0].Value)
	}
}

func TestTranslateMetrics_Histogram(t *testing.T) {
	bucketCounts := []uint64{5, 20, 35}
	explicitBounds := []float64{10.0, 50.0, 100.0}

	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test"},
						Metrics: []*metricspb.Metric{
							{
								Name: "http_request_duration",
								Data: &metricspb.Metric_Histogram{
									Histogram: &metricspb.Histogram{
										DataPoints: []*metricspb.HistogramDataPoint{
											{
												TimeUnixNano:   1717000000000000000,
												Count:          60,
												Sum:            func() *float64 { v := 7500.0; return &v }(),
												BucketCounts:   bucketCounts,
												ExplicitBounds: explicitBounds,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	points := TranslateMetrics(req)
	// Expected: 3 buckets + 1 _sum + 1 _count = 5 points
	if len(points) != 5 {
		t.Fatalf("expected 5 points (3 buckets + sum + count), got %d", len(points))
	}

	// Check _sum and _count exist.
	sumFound := false
	countFound := false
	for _, p := range points {
		if p.Name == "http_request_duration_sum" {
			sumFound = true
			if p.Value != 7500.0 {
				t.Errorf("expected sum 7500, got %f", p.Value)
			}
		}
		if p.Name == "http_request_duration_count" {
			countFound = true
			if p.Value != 60.0 {
				t.Errorf("expected count 60, got %f", p.Value)
			}
		}
	}
	if !sumFound {
		t.Error("no _sum point found")
	}
	if !countFound {
		t.Error("no _count point found")
	}
}

func TestTranslateMetrics_EmptyRequest(t *testing.T) {
	req := &colmetricspb.ExportMetricsServiceRequest{}
	points := TranslateMetrics(req)
	if points != nil && len(points) > 0 {
		t.Errorf("expected 0 points for empty request, got %d", len(points))
	}
}
