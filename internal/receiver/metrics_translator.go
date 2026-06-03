package receiver

import (
	"fmt"

	"github.com/labubu/labubu/internal/metrics"
	colmetricspb "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
)

// TranslateMetrics converts an OTLP ExportMetricsServiceRequest into a flat
// list of MetricPoints using Prometheus data model conventions.
func TranslateMetrics(req *colmetricspb.ExportMetricsServiceRequest) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, rm := range req.GetResourceMetrics() {
		resourceLabels := keyValueToMap(rm.GetResource().GetAttributes())

		// Extract service.name as "service" label for Prometheus compatibility.
		if svc, ok := resourceLabels["service.name"]; ok {
			resourceLabels["service"] = svc
		}

		for _, sm := range rm.GetScopeMetrics() {
			scopeLabels := map[string]string{
				"scope_name":    sm.GetScope().GetName(),
				"scope_version": sm.GetScope().GetVersion(),
			}

			for _, m := range sm.GetMetrics() {
				if m == nil {
					continue
				}
				switch d := m.Data.(type) {
				case *metricspb.Metric_Gauge:
					points = append(points, gaugeToPoints(m.Name, resourceLabels, scopeLabels, d.Gauge)...)
				case *metricspb.Metric_Sum:
					points = append(points, sumToPoints(m.Name, resourceLabels, scopeLabels, d.Sum)...)
				case *metricspb.Metric_Histogram:
					points = append(points, histogramToPoints(m.Name, resourceLabels, scopeLabels, d.Histogram)...)
				case *metricspb.Metric_Summary:
					points = append(points, summaryToPoints(m.Name, resourceLabels, scopeLabels, d.Summary)...)
				default:
					// Unknown data type — skip, don't block.
				}
			}
		}
	}

	return points
}

// gaugeToPoints translates OTLP Gauge data points.
func gaugeToPoints(name string, resourceLabels, scopeLabels map[string]string, gauge *metricspb.Gauge) []metrics.MetricPoint {
	var points []metrics.MetricPoint
	for _, dp := range gauge.GetDataPoints() {
		pts := numberDataPointToMetricPoints(name, resourceLabels, scopeLabels, dp)
		points = append(points, pts...)
	}
	return points
}

// sumToPoints translates OTLP Sum data points.
func sumToPoints(name string, resourceLabels, scopeLabels map[string]string, sum *metricspb.Sum) []metrics.MetricPoint {
	var points []metrics.MetricPoint
	for _, dp := range sum.GetDataPoints() {
		pts := numberDataPointToMetricPoints(name, resourceLabels, scopeLabels, dp)
		points = append(points, pts...)
	}
	return points
}

// numberDataPointToMetricPoints converts an OTLP NumberDataPoint to []MetricPoint.
func numberDataPointToMetricPoints(name string, resourceLabels, scopeLabels map[string]string, dp *metricspb.NumberDataPoint) []metrics.MetricPoint {
	if dp == nil {
		return nil
	}

	ts := int64(dp.GetTimeUnixNano() / 1_000_000) // nanoseconds → milliseconds

	attrLabels := keyValueToMap(dp.GetAttributes())
	// Merge labels: scope < resource < attribute (later overrides).
	allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

	var value float64
	switch v := dp.Value.(type) {
	case *metricspb.NumberDataPoint_AsInt:
		value = float64(v.AsInt)
	case *metricspb.NumberDataPoint_AsDouble:
		value = v.AsDouble
	}

	return []metrics.MetricPoint{{
		Name:      name,
		Labels:    allLabels,
		Value:     value,
		Timestamp: ts,
	}}
}

// histogramToPoints expands an OTLP Histogram into Prometheus-style _bucket, _sum, _count points.
func histogramToPoints(name string, resourceLabels, scopeLabels map[string]string, hist *metricspb.Histogram) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, dp := range hist.GetDataPoints() {
		ts := int64(dp.GetTimeUnixNano() / 1_000_000) // ns → ms
		attrLabels := keyValueToMap(dp.GetAttributes())
		allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

		bounds := dp.GetExplicitBounds()
		counts := dp.GetBucketCounts()

		// Emit _bucket points with le label.
		cumulative := uint64(0)
		for i := 0; i < len(bounds) && i < len(counts); i++ {
			cumulative += counts[i]
			bucketLabels := copyLabels(allLabels)
			bucketLabels["le"] = fmt.Sprintf("%f", bounds[i])
			points = append(points, metrics.MetricPoint{
				Name:      name + "_bucket",
				Labels:    bucketLabels,
				Value:     float64(cumulative),
				Timestamp: ts,
			})
		}

		// Emit +Inf bucket only when there are counts beyond explicit bounds.
		if len(counts) > len(bounds) {
			cumulative += counts[len(bounds)]
			infLabels := copyLabels(allLabels)
			infLabels["le"] = "+Inf"
			points = append(points, metrics.MetricPoint{
				Name:      name + "_bucket",
				Labels:    infLabels,
				Value:     float64(cumulative),
				Timestamp: ts,
			})
		}

		// Emit _sum.
		if dp.Sum != nil {
			sumLabels := copyLabels(allLabels)
			points = append(points, metrics.MetricPoint{
				Name:      name + "_sum",
				Labels:    sumLabels,
				Value:     *dp.Sum,
				Timestamp: ts,
			})
		}

		// Emit _count.
		countLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_count",
			Labels:    countLabels,
			Value:     float64(dp.GetCount()),
			Timestamp: ts,
		})
	}

	return points
}

// summaryToPoints translates an OTLP Summary into Prometheus _sum, _count, and quantile points.
func summaryToPoints(name string, resourceLabels, scopeLabels map[string]string, summary *metricspb.Summary) []metrics.MetricPoint {
	var points []metrics.MetricPoint

	for _, dp := range summary.GetDataPoints() {
		ts := int64(dp.GetTimeUnixNano() / 1_000_000) // ns → ms
		attrLabels := keyValueToMap(dp.GetAttributes())
		allLabels := mergeLabels(resourceLabels, scopeLabels, attrLabels)

		// Emit _sum.
		sumLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_sum",
			Labels:    sumLabels,
			Value:     dp.GetSum(),
			Timestamp: ts,
		})

		// Emit _count.
		countLabels := copyLabels(allLabels)
		points = append(points, metrics.MetricPoint{
			Name:      name + "_count",
			Labels:    countLabels,
			Value:     float64(dp.GetCount()),
			Timestamp: ts,
		})

		// Emit quantile points.
		for _, qv := range dp.GetQuantileValues() {
			qLabels := copyLabels(allLabels)
			qLabels["quantile"] = fmt.Sprintf("%f", qv.GetQuantile())
			points = append(points, metrics.MetricPoint{
				Name:      name,
				Labels:    qLabels,
				Value:     qv.GetValue(),
				Timestamp: ts,
			})
		}
	}

	return points
}

// mergeLabels merges multiple label maps. Later maps override earlier ones for the same key.
func mergeLabels(maps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			if v != "" {
				result[k] = v
			}
		}
	}
	return result
}

// copyLabels returns a shallow copy of a labels map.
func copyLabels(labels map[string]string) map[string]string {
	cp := make(map[string]string, len(labels))
	for k, v := range labels {
		cp[k] = v
	}
	return cp
}
