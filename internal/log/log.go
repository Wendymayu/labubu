// Package log provides a minimal leveled logger wrapping the standard log package.
package log

import (
	"fmt"
	stdlog "log"
	"strings"
)

// Level represents a log severity level.
type Level int

const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
)

var currentLevel = INFO

// SetLevel sets the minimum log level. Messages below this level are suppressed.
func SetLevel(l Level) {
	currentLevel = l
}

// ParseLevel parses a level string (case-insensitive).
func ParseLevel(s string) (Level, error) {
	switch strings.ToLower(s) {
	case "debug":
		return DEBUG, nil
	case "info":
		return INFO, nil
	case "warn", "warning":
		return WARN, nil
	case "error":
		return ERROR, nil
	default:
		return INFO, fmt.Errorf("unknown log level %q (valid: debug, info, warn, error)", s)
	}
}

// Debug logs a message at DEBUG level.
func Debug(format string, v ...interface{}) {
	if currentLevel <= DEBUG {
		stdlog.Printf("[DEBUG] "+format, v...)
	}
}

// Info logs a message at INFO level.
func Info(format string, v ...interface{}) {
	if currentLevel <= INFO {
		stdlog.Printf("[INFO] "+format, v...)
	}
}

// Warn logs a message at WARN level.
func Warn(format string, v ...interface{}) {
	if currentLevel <= WARN {
		stdlog.Printf("[WARN] "+format, v...)
	}
}

// Error logs a message at ERROR level.
func Error(format string, v ...interface{}) {
	if currentLevel <= ERROR {
		stdlog.Printf("[ERROR] "+format, v...)
	}
}
