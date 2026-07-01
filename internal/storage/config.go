package storage

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Labubu configuration loaded from YAML.
type Config struct {
	Trace   TraceConfig   `yaml:"trace"`
	Log     LogConfig     `yaml:"log"`
	Metric  MetricConfig  `yaml:"metric"`
	Pricing PricingConfig `yaml:"pricing"`
}

// TraceConfig holds trace-specific configuration.
type TraceConfig struct {
	Retention RetentionConfig `yaml:"retention"`
}

// RetentionConfig controls how old trace data is cleaned up.
type RetentionConfig struct {
	MaxAge          time.Duration // traces older than this are deleted
	MaxCount        int           // keep only the newest MaxCount traces; 0 = unlimited
	CleanupInterval time.Duration // how often the cleanup goroutine runs
}

// LogConfig holds log-specific configuration.
type LogConfig struct {
	Retention LogRetentionConfig `yaml:"retention"`
}

// LogRetentionConfig controls how old log records are cleaned up.
type LogRetentionConfig struct {
	MaxAge time.Duration // logs older than this are deleted
}

// MetricConfig holds metric-specific configuration.
type MetricConfig struct {
	Retention MetricRetentionConfig `yaml:"retention"`
}

// MetricRetentionConfig controls how long metric data is retained.
type MetricRetentionConfig struct {
	MaxAge time.Duration // metrics older than this are dropped by tstorage
}

// yamlConfig mirrors Config but uses string fields for durations
// so that YAML values like "24h" are parsed via time.ParseDuration.
type yamlConfig struct {
	Trace struct {
		Retention struct {
			MaxAge          string `yaml:"max_age"`
			MaxCount        *int   `yaml:"max_count"`
			CleanupInterval string `yaml:"cleanup_interval"`
		} `yaml:"retention"`
	} `yaml:"trace"`
	Log struct {
		Retention struct {
			MaxAge string `yaml:"max_age"`
		} `yaml:"retention"`
	} `yaml:"log"`
	Metric struct {
		Retention struct {
			MaxAge string `yaml:"max_age"`
		} `yaml:"retention"`
	} `yaml:"metric"`
	Pricing struct {
		Models []struct {
			Name        string  `yaml:"name"`
			InputPrice  float64 `yaml:"input_price"`
			OutputPrice float64 `yaml:"output_price"`
			Currency    string  `yaml:"currency"`
		} `yaml:"models"`
	} `yaml:"pricing"`
}

// DefaultConfig returns a Config with production defaults.
func DefaultConfig() Config {
	return Config{
		Trace: TraceConfig{
			Retention: RetentionConfig{
				MaxAge:          7 * 24 * time.Hour,
				MaxCount:        10000,
				CleanupInterval: 5 * time.Minute,
			},
		},
		Log: LogConfig{
			Retention: LogRetentionConfig{
				MaxAge: 7 * 24 * time.Hour,
			},
		},
		Metric: MetricConfig{
			Retention: MetricRetentionConfig{
				MaxAge: 7 * 24 * time.Hour,
			},
		},
		Pricing: PricingConfig{
			Models: []ModelPricing{
				{ModelName: "claude-opus-4-8", InputPrice: 15.0, OutputPrice: 75.0, Currency: "USD"},
				{ModelName: "claude-sonnet-4-6", InputPrice: 3.0, OutputPrice: 15.0, Currency: "USD"},
				{ModelName: "claude-haiku-4-5", InputPrice: 0.80, OutputPrice: 4.0, Currency: "USD"},
			},
		},
	}
}

// LoadConfig reads a YAML config file at path and returns a Config.
// If the file does not exist, DefaultConfig is returned silently.
// If the file contains invalid YAML, a warning is logged and defaults are returned.
func LoadConfig(path string) Config {
	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}

	var raw yamlConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		log.Printf("Warning: failed to parse %s: %v (using defaults)", path, err)
		return DefaultConfig()
	}

	if raw.Trace.Retention.MaxAge != "" {
		if d, err := time.ParseDuration(raw.Trace.Retention.MaxAge); err == nil {
			cfg.Trace.Retention.MaxAge = d
		}
	}
	if raw.Trace.Retention.MaxCount != nil {
		cfg.Trace.Retention.MaxCount = *raw.Trace.Retention.MaxCount
	}
	if raw.Trace.Retention.CleanupInterval != "" {
		if d, err := time.ParseDuration(raw.Trace.Retention.CleanupInterval); err == nil {
			cfg.Trace.Retention.CleanupInterval = d
		}
	}

	if raw.Log.Retention.MaxAge != "" {
		if d, err := time.ParseDuration(raw.Log.Retention.MaxAge); err == nil {
			cfg.Log.Retention.MaxAge = d
		}
	}

	if raw.Metric.Retention.MaxAge != "" {
		if d, err := time.ParseDuration(raw.Metric.Retention.MaxAge); err == nil {
			cfg.Metric.Retention.MaxAge = d
		}
	}

	for _, m := range raw.Pricing.Models {
		cfg.Pricing.Models = append(cfg.Pricing.Models, ModelPricing{
			ModelName: m.Name, InputPrice: m.InputPrice,
			OutputPrice: m.OutputPrice, Currency: m.Currency,
		})
	}

	return cfg
}
