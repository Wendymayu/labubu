// Package metrics defines the metrics storage interface and data types.
package metrics

import "context"

// MetricPoint is a single metric data point with labels.
type MetricPoint struct {
	Name      string
	Labels    map[string]string
	Value     float64
	Timestamp int64 // milliseconds since epoch
}

// MetricSeries is a named set of labeled data points (a time series).
type MetricSeries struct {
	Name   string
	Labels map[string]string
	Points []MetricPoint
}

// Store is the metrics storage backend interface.
type Store interface {
	// Insert writes metric data points. Called by the metrics receiver.
	Insert(ctx context.Context, points []MetricPoint) error

	// Select returns time series matching the metric name and label filters
	// within the time range [start, end] (milliseconds since epoch).
	Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]MetricSeries, error)

	// LabelNames returns all known label names.
	LabelNames(ctx context.Context) ([]string, error)

	// LabelValues returns all values for a given label name.
	LabelValues(ctx context.Context, name string) ([]string, error)

	// Close gracefully shuts down the store.
	Close() error
}
