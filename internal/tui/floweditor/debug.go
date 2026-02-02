package floweditor

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DebugLevel represents the verbosity level of debug logging
type DebugLevel int

const (
	DebugLevelOff DebugLevel = iota
	DebugLevelError
	DebugLevelWarn
	DebugLevelInfo
	DebugLevelDebug
	DebugLevelTrace
)

// Logger handles debug logging for the flow editor
type Logger struct {
	level   DebugLevel
	file    *os.File
	mu      sync.Mutex
	enabled bool
	logPath string
}

var (
	globalLogger *Logger
	once         sync.Once
)

// GetLogger returns the global logger instance
func GetLogger() *Logger {
	once.Do(func() {
		globalLogger = &Logger{
			level:   DebugLevelOff,
			enabled: false,
		}
	})
	return globalLogger
}

// InitLogger initializes the logger with a specific level and log file
func InitLogger(level DebugLevel, logPath string) error {
	logger := GetLogger()
	logger.mu.Lock()
	defer logger.mu.Unlock()

	logger.level = level
	logger.enabled = level > DebugLevelOff
	logger.logPath = logPath

	if !logger.enabled {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(logPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// Open log file
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	logger.file = file
	logger.log("LOGGER", fmt.Sprintf("Debug logging initialized at level %s", level.String()))

	return nil
}

// Close closes the log file
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file != nil {
		l.log("LOGGER", "Debug logging stopped")
		return l.file.Close()
	}
	return nil
}

// IsEnabled returns true if logging is enabled
func (l *Logger) IsEnabled() bool {
	return l.enabled
}

// GetLevel returns the current debug level
func (l *Logger) GetLevel() DebugLevel {
	return l.level
}

// log writes a log entry
func (l *Logger) log(level string, message string) {
	if l.file == nil {
		return
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	_, file, line, _ := runtime.Caller(2)
	file = filepath.Base(file)

	logLine := fmt.Sprintf("[%s] [%s] [%s:%d] %s\n", timestamp, level, file, line, message)
	l.file.WriteString(logLine)
	l.file.Sync()
}

// shouldLog returns true if the given level should be logged
func (l *Logger) shouldLog(level DebugLevel) bool {
	return l.enabled && level <= l.level
}

// Error logs an error message
func (l *Logger) Error(format string, args ...interface{}) {
	if !l.shouldLog(DebugLevelError) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("ERROR", fmt.Sprintf(format, args...))
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...interface{}) {
	if !l.shouldLog(DebugLevelWarn) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("WARN", fmt.Sprintf(format, args...))
}

// Info logs an info message
func (l *Logger) Info(format string, args ...interface{}) {
	if !l.shouldLog(DebugLevelInfo) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("INFO", fmt.Sprintf(format, args...))
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...interface{}) {
	if !l.shouldLog(DebugLevelDebug) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("DEBUG", fmt.Sprintf(format, args...))
}

// Trace logs a trace message (very verbose)
func (l *Logger) Trace(format string, args ...interface{}) {
	if !l.shouldLog(DebugLevelTrace) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("TRACE", fmt.Sprintf(format, args...))
}

// LogRequest logs an HTTP request
func (l *Logger) LogRequest(method, url string, headers map[string]string, body string) {
	if !l.shouldLog(DebugLevelDebug) {
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("REQUEST: %s %s\n", method, url))
	sb.WriteString("Headers:\n")
	for k, v := range headers {
		sb.WriteString(fmt.Sprintf("  %s: %s\n", k, v))
	}
	if body != "" {
		sb.WriteString(fmt.Sprintf("Body: %s\n", body))
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("REQUEST", sb.String())
}

// LogResponse logs an HTTP response
func (l *Logger) LogResponse(statusCode int, status string, body string, duration time.Duration) {
	if !l.shouldLog(DebugLevelDebug) {
		return
	}

	msg := fmt.Sprintf("RESPONSE: %d %s (took %v)\nBody: %s", statusCode, status, duration, body)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("RESPONSE", msg)
}

// LogState logs a state change
func (l *Logger) LogState(component string, oldState, newState interface{}) {
	if !l.shouldLog(DebugLevelTrace) {
		return
	}

	msg := fmt.Sprintf("%s state change: %+v -> %+v", component, oldState, newState)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("STATE", msg)
}

// LogKey logs a key press
func (l *Logger) LogKey(key string, mode EditorMode) {
	if !l.shouldLog(DebugLevelTrace) {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("KEY", fmt.Sprintf("Key pressed: '%s' in mode: %s", key, mode.String()))
}

// LogNode logs node operations
func (l *Logger) LogNode(operation string, node *Node) {
	if !l.shouldLog(DebugLevelDebug) {
		return
	}

	msg := fmt.Sprintf("%s node: ID=%s, Type=%s, Name=%s, Pos=(%d,%d)",
		operation, node.ID.String(), node.Type, node.Name, node.X, node.Y)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("NODE", msg)
}

// LogEdge logs edge operations
func (l *Logger) LogEdge(operation string, edge *Edge) {
	if !l.shouldLog(DebugLevelDebug) {
		return
	}

	msg := fmt.Sprintf("%s edge: ID=%s, From=%s, To=%s, Type=%s",
		operation, edge.ID.String(), edge.From.String(), edge.To.String(), edge.Type)

	l.mu.Lock()
	defer l.mu.Unlock()
	l.log("EDGE", msg)
}

// String returns the string representation of a debug level
func (d DebugLevel) String() string {
	switch d {
	case DebugLevelOff:
		return "OFF"
	case DebugLevelError:
		return "ERROR"
	case DebugLevelWarn:
		return "WARN"
	case DebugLevelInfo:
		return "INFO"
	case DebugLevelDebug:
		return "DEBUG"
	case DebugLevelTrace:
		return "TRACE"
	default:
		return "UNKNOWN"
	}
}

// ParseDebugLevel parses a debug level from string
func ParseDebugLevel(s string) DebugLevel {
	switch strings.ToUpper(s) {
	case "OFF":
		return DebugLevelOff
	case "ERROR":
		return DebugLevelError
	case "WARN":
		return DebugLevelWarn
	case "INFO":
		return DebugLevelInfo
	case "DEBUG":
		return DebugLevelDebug
	case "TRACE":
		return DebugLevelTrace
	default:
		return DebugLevelOff
	}
}
