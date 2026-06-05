package storage

import (
	"log"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level Labubu configuration loaded from YAML.
type Config struct {
	Trace TraceConfig `yaml:"trace"`
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
}

// DefaultConfig returns a Config with production defaults.
func DefaultConfig() Config {
	return Config{
		Trace: TraceConfig{
			Retention: RetentionConfig{
				MaxAge:          24 * time.Hour,
				MaxCount:        10000,
				CleanupInterval: 5 * time.Minute,
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

	return cfg
}
