// +build ignore

package main

import (
	"bytes"
	"fmt"
	"net/http"
	"time"

	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/protobuf/proto"
)

func main() {
	nowNs := uint64(time.Now().UnixNano())

	req := &colmetricspb.ExportMetricsServiceRequest{
		ResourceMetrics: []*metricspb.ResourceMetrics{{
			Resource: &resourcepb.Resource{
				Attributes: []*commonpb.KeyValue{
					{Key: "service.name", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "agent-gateway"}}},
				},
			},
			ScopeMetrics: []*metricspb.ScopeMetrics{{
				Scope: &commonpb.InstrumentationScope{Name: "test", Version: "1.0"},
				Metrics: []*metricspb.Metric{
					{
						Name: "gen_ai.client.token.usage",
						Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
							DataPoints: []*metricspb.NumberDataPoint{{
								TimeUnixNano: nowNs,
								Attributes: []*commonpb.KeyValue{
									{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
								},
								Value: &metricspb.NumberDataPoint_AsInt{AsInt: 4500},
							}},
						}},
					},
					{
						Name: "gen_ai.client.token.usage",
						Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
							DataPoints: []*metricspb.NumberDataPoint{{
								TimeUnixNano: nowNs - uint64(60*time.Second),
								Attributes: []*commonpb.KeyValue{
									{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
								},
								Value: &metricspb.NumberDataPoint_AsInt{AsInt: 3200},
							}},
						}},
					},
					{
						Name: "gen_ai.client.token.usage",
						Data: &metricspb.Metric_Gauge{Gauge: &metricspb.Gauge{
							DataPoints: []*metricspb.NumberDataPoint{{
								TimeUnixNano: nowNs - uint64(120*time.Second),
								Attributes: []*commonpb.KeyValue{
									{Key: "model", Value: &commonpb.AnyValue{Value: &commonpb.AnyValue_StringValue{StringValue: "claude-opus-4-8"}}},
								},
								Value: &metricspb.NumberDataPoint_AsInt{AsInt: 2800},
							}},
						}},
					},
				},
			}},
		}},
	}

	body, err := proto.Marshal(req)
	if err != nil {
		fmt.Println("Marshal error:", err)
		return
	}

	resp, err := http.Post("http://localhost:8080/api/v1/otlp/v1/metrics", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	defer resp.Body.Close()
	fmt.Printf("Sent %d bytes -> HTTP %d\n", len(body), resp.StatusCode)
}
