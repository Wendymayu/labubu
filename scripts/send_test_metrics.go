// +build ignore

package main

import (
	"fmt"
	"os"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
	"net/http"
	"bytes"
)

func main() {
	// 构造一条 Gauge 指标：gen_ai_client_token_usage
	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{
			{
				Resource: &resourcepb.Resource{
					Attributes: []*commonpb.KeyValue{
						{Key: "service.name", Value: &commonpb.AnyValue{
							Value: &commonpb.AnyValue_StringValue{StringValue: "agent-gateway"},
						}},
					},
				},
				ScopeMetrics: []*metricspb.ScopeMetrics{
					{
						Scope: &commonpb.InstrumentationScope{Name: "test-scope", Version: "1.0"},
						Metrics: []*metricspb.Metric{
							// 1. Gauge: token 用量
							{
								Name: "gen_ai_client_token_usage",
								Data: &metricspb.Metric_Gauge{
									Gauge: &metricspb.Gauge{
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1748937600000000000,
												Attributes: []*commonpb.KeyValue{
													{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
												},
												Value: &metricspb.NumberDataPoint_AsInt{AsInt: 4500},
											},
										},
									},
								},
							},
							// 2. Histogram: 请求延迟
							{
								Name: "gen_ai_client_request_duration_seconds",
								Data: &metricspb.Metric_Histogram{
									Histogram: &metricspb.Histogram{
										DataPoints: []*metricspb.HistogramDataPoint{
											{
												TimeUnixNano: 1748937600000000000,
												Count:         100,
												Sum:           func() *float64 { v := 120.5; return &v }(),
												BucketCounts:   []uint64{10, 30, 40, 15, 5},
												ExplicitBounds: []float64{0.5, 1.0, 2.0, 5.0},
												Attributes: []*commonpb.KeyValue{
													{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
												},
											},
										},
									},
								},
							},
							// 3. Sum: 请求计数
							{
								Name: "gen_ai_client_requests_total",
								Data: &metricspb.Metric_Sum{
									Sum: &metricspb.Sum{
										AggregationTemporality: metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE,
										IsMonotonic:            true,
										DataPoints: []*metricspb.NumberDataPoint{
											{
												TimeUnixNano: 1748937600000000000,
												Value:        &metricspb.NumberDataPoint_AsInt{AsInt: 9999},
												Attributes: []*commonpb.KeyValue{
													{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
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
		},
	}

	// 序列化为 protobuf
	body, err := proto.Marshal(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal error: %v\n", err)
		os.Exit(1)
	}

	// 发送到 API 端口（跟普罗米修斯一致）
	url := "http://localhost:8080/api/v1/otlp/v1/metrics"
	resp, err := http.Post(url, "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "send error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Printf("发送 %d bytes → %s\n", len(body), url)
	fmt.Printf("响应: HTTP %d\n", resp.StatusCode)
}
