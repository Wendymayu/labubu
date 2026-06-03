package metrics

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/nakabonne/tstorage"
)

// TStorageConfig holds configuration for the tstorage-backed metrics store.
type TStorageConfig struct {
	DataDir   string        // empty = in-memory only
	Retention time.Duration // how long to retain data in memory partitions
}

// TStorageStore implements Store using the tstorage embedded TSDB.
type TStorageStore struct {
	storage  tstorage.Storage
	labelIdx map[string]map[string]struct{} // label name -> values set
	mu       sync.RWMutex
}

// NewTStorageStore creates a new tstorage-backed metrics store.
func NewTStorageStore(cfg TStorageConfig) (*TStorageStore, error) {
	opts := []tstorage.Option{
		tstorage.WithTimestampPrecision(tstorage.Milliseconds),
	}
	if cfg.DataDir != "" {
		opts = append(opts, tstorage.WithDataPath(cfg.DataDir))
	}
	if cfg.Retention > 0 {
		opts = append(opts, tstorage.WithRetention(cfg.Retention))
	}

	s, err := tstorage.NewStorage(opts...)
	if err != nil {
		return nil, fmt.Errorf("tstorage: %w", err)
	}

	return &TStorageStore{
		storage:  s,
		labelIdx: make(map[string]map[string]struct{}),
	}, nil
}

// Insert writes metric data points to the store.
func (s *TStorageStore) Insert(ctx context.Context, points []MetricPoint) error {
	rows := make([]tstorage.Row, 0, len(points))
	for _, p := range points {
		labels := make([]tstorage.Label, 0, len(p.Labels))
		for k, v := range p.Labels {
			labels = append(labels, tstorage.Label{Name: k, Value: v})
		}
		rows = append(rows, tstorage.Row{
			Metric: p.Name,
			Labels: labels,
			DataPoint: tstorage.DataPoint{
				Value:     p.Value,
				Timestamp: p.Timestamp,
			},
		})
	}

	if err := s.storage.InsertRows(rows); err != nil {
		return fmt.Errorf("tstorage insert: %w", err)
	}

	// Update label index.
	s.mu.Lock()
	for _, p := range points {
		// Track metric name as __name__ label.
		if s.labelIdx["__name__"] == nil {
			s.labelIdx["__name__"] = make(map[string]struct{})
		}
		s.labelIdx["__name__"][p.Name] = struct{}{}

		for k, v := range p.Labels {
			if s.labelIdx[k] == nil {
				s.labelIdx[k] = make(map[string]struct{})
			}
			s.labelIdx[k][v] = struct{}{}
		}
	}
	s.mu.Unlock()

	return nil
}

// Select returns time series matching the metric name and label filters.
func (s *TStorageStore) Select(ctx context.Context, metric string, labels map[string]string, start, end int64) ([]MetricSeries, error) {
	tlabels := make([]tstorage.Label, 0, len(labels))
	for k, v := range labels {
		tlabels = append(tlabels, tstorage.Label{Name: k, Value: v})
	}

	dps, err := s.storage.Select(metric, tlabels, start, end)
	if err != nil {
		if errors.Is(err, tstorage.ErrNoDataPoints) {
			return nil, nil
		}
		return nil, fmt.Errorf("tstorage select: %w", err)
	}

	if len(dps) == 0 {
		return nil, nil
	}

	// Sort by timestamp.
	sort.Slice(dps, func(i, j int) bool {
		return dps[i].Timestamp < dps[j].Timestamp
	})

	mpoints := make([]MetricPoint, 0, len(dps))
	for _, p := range dps {
		mpoints = append(mpoints, MetricPoint{
			Name:      metric,
			Labels:    labels,
			Value:     p.Value,
			Timestamp: p.Timestamp,
		})
	}

	return []MetricSeries{{
		Name:   metric,
		Labels: labels,
		Points: mpoints,
	}}, nil
}

// LabelNames returns all known label names.
func (s *TStorageStore) LabelNames(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, 0, len(s.labelIdx))
	for k := range s.labelIdx {
		names = append(names, k)
	}
	sort.Strings(names)
	return names, nil
}

// LabelValues returns all values for a given label name.
func (s *TStorageStore) LabelValues(ctx context.Context, name string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]string, 0)
	if vs, ok := s.labelIdx[name]; ok {
		for v := range vs {
			values = append(values, v)
		}
	}
	sort.Strings(values)
	return values, nil
}

// Close shuts down the store.
func (s *TStorageStore) Close() error {
	return s.storage.Close()
}
