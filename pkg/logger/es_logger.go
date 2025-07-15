package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"sync"
	"time"
)

// LogLevel represents the severity level of a log entry
type LogLevel int

const (
	DEBUG LogLevel = iota
	INFO
	WARN
	ERROR
	FATAL
)

// String returns the string representation of log level
func (l LogLevel) String() string {
	switch l {
	case DEBUG:
		return "DEBUG"
	case INFO:
		return "INFO"
	case WARN:
		return "WARN"
	case ERROR:
		return "ERROR"
	case FATAL:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// LogEntry represents a single log entry with all required fields
type LogEntry struct {
	Component     string      `json:"component"`
	Action        string      `json:"action"`
	Code          string      `json:"code"`
	Message       string      `json:"message"`
	CustomMessage string      `json:"custommessage"`
	LogDate       time.Time   `json:"logdate"`
	UserID        string      `json:"userid"`
	Type          string      `json:"type"`
	Data          interface{} `json:"data"`
}

// Config holds the configuration for the logger
type Config struct {
	URL             string        // Elasticsearch endpoint URL
	MaxRetries      int           // Maximum number of retry attempts
	RetryDelay      time.Duration // Delay between retries
	BatchSize       int           // Size of batch for bulk operations
	FlushInterval   time.Duration // Interval to flush logs
	HTTPTimeout     time.Duration // HTTP client timeout
	MaxQueueSize    int           // Maximum queue size before dropping logs
	EnableAsync     bool          // Enable asynchronous logging
	ComponentName   string        // Default component name
	BufferSize      int           // Buffer size for async channel
}

// DefaultConfig returns a default configuration
func DefaultConfig(elasticURL string) *Config {
	return &Config{
		URL:           elasticURL,
		MaxRetries:    3,
		RetryDelay:    time.Second,
		BatchSize:     100,
		FlushInterval: 10 * time.Second,
		HTTPTimeout:   30 * time.Second,
		MaxQueueSize:  10000,
		EnableAsync:   true,
		ComponentName: "default",
		BufferSize:    1000,
	}
}

// ElasticLogger represents the main logger instance
type ElasticLogger struct {
	config     *Config
	client     *http.Client
	queue      chan *LogEntry
	batch      []*LogEntry
	mu         sync.RWMutex
	ctx        context.Context
	cancel     context.CancelFunc
	wg         sync.WaitGroup
	closed     bool
	closeMu    sync.RWMutex
	ticker     *time.Ticker
	stats      *LoggerStats
	errorsCh   chan error
}

// LoggerStats holds statistics about the logger
type LoggerStats struct {
	TotalLogs    int64
	SuccessLogs  int64
	FailedLogs   int64
	DroppedLogs  int64
	RetryCount   int64
	mu           sync.RWMutex
}

// GetStats returns a copy of current logger statistics
func (s *LoggerStats) GetStats() LoggerStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return *s
}

// NewElasticLogger creates a new instance of ElasticLogger
func NewElasticLogger(config *Config) (*ElasticLogger, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}

	if config.URL == "" {
		return nil, fmt.Errorf("elasticsearch URL cannot be empty")
	}

	// Validate configuration
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())

	logger := &ElasticLogger{
		config: config,
		client: &http.Client{
			Timeout: config.HTTPTimeout,
		},
		queue:    make(chan *LogEntry, config.BufferSize),
		batch:    make([]*LogEntry, 0, config.BatchSize),
		ctx:      ctx,
		cancel:   cancel,
		stats:    &LoggerStats{},
		errorsCh: make(chan error, 100),
	}

	if config.EnableAsync {
		logger.ticker = time.NewTicker(config.FlushInterval)
		logger.wg.Add(1)
		go logger.processLogs()
	}

	return logger, nil
}

// LogOptions provides options for logging
type LogOptions struct {
	Component     string
	Action        string
	Code          string
	Message       string
	CustomMessage string
	UserID        string
	Type          string
	Data          interface{}
	Level         LogLevel
}

// Log logs an entry with the given options
func (el *ElasticLogger) Log(opts LogOptions) error {
	return el.LogWithContext(context.Background(), opts)
}

// LogWithContext logs an entry with context
func (el *ElasticLogger) LogWithContext(ctx context.Context, opts LogOptions) error {
	el.closeMu.RLock()
	if el.closed {
		el.closeMu.RUnlock()
		return fmt.Errorf("logger is closed")
	}
	el.closeMu.RUnlock()

	// Set default component if not provided
	if opts.Component == "" {
		opts.Component = el.config.ComponentName
	}

	// Set default type based on level
	if opts.Type == "" {
		opts.Type = opts.Level.String()
	}

	entry := &LogEntry{
		Component:     opts.Component,
		Action:        opts.Action,
		Code:          opts.Code,
		Message:       opts.Message,
		CustomMessage: opts.CustomMessage,
		LogDate:       time.Now().UTC(),
		UserID:        opts.UserID,
		Type:          opts.Type,
		Data:          opts.Data,
	}

	el.stats.mu.Lock()
	el.stats.TotalLogs++
	el.stats.mu.Unlock()

	if el.config.EnableAsync {
		select {
		case el.queue <- entry:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Queue is full, drop the log
			el.stats.mu.Lock()
			el.stats.DroppedLogs++
			el.stats.mu.Unlock()
			return fmt.Errorf("log queue is full, dropping log entry")
		}
	}

	// Synchronous logging
	return el.sendLogEntry(entry)
}

// Convenience methods for different log levels
func (el *ElasticLogger) Debug(component, action, message string, data interface{}) error {
	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   message,
		Data:      data,
		Level:     DEBUG,
	})
}

func (el *ElasticLogger) Info(component, action, message string, data interface{}) error {
	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   message,
		Data:      data,
		Level:     INFO,
	})
}

func (el *ElasticLogger) Warn(component, action, message string, data interface{}) error {
	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   message,
		Data:      data,
		Level:     WARN,
	})
}

func (el *ElasticLogger) Error(component, action, message string, data interface{}) error {
	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   message,
		Data:      data,
		Level:     ERROR,
	})
}

func (el *ElasticLogger) Fatal(component, action, message string, data interface{}) error {
	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   message,
		Data:      data,
		Level:     FATAL,
	})
}

// processLogs processes logs asynchronously
func (el *ElasticLogger) processLogs() {
	defer el.wg.Done()
	defer el.ticker.Stop()

	for {
		select {
		case <-el.ctx.Done():
			// Flush remaining logs before shutdown
			el.flushBatch()
			return

		case entry := <-el.queue:
			el.mu.Lock()
			el.batch = append(el.batch, entry)
			
			if len(el.batch) >= el.config.BatchSize {
				el.flushBatch()
			}
			el.mu.Unlock()

		case <-el.ticker.C:
			el.mu.Lock()
			if len(el.batch) > 0 {
				el.flushBatch()
			}
			el.mu.Unlock()
		}
	}
}

// flushBatch sends the current batch to Elasticsearch
func (el *ElasticLogger) flushBatch() {
	if len(el.batch) == 0 {
		return
	}

	batch := make([]*LogEntry, len(el.batch))
	copy(batch, el.batch)
	el.batch = el.batch[:0] // Clear the batch

	// Send batch in a separate goroutine to avoid blocking
	go func(entries []*LogEntry) {
		if err := el.sendLogEntries(entries); err != nil {
			// Send error to error channel for monitoring
			select {
			case el.errorsCh <- err:
			default:
				// Error channel is full, drop the error
			}
		}
	}(batch)
}

// sendLogEntry sends a single log entry to Elasticsearch
func (el *ElasticLogger) sendLogEntry(entry *LogEntry) error {
	return el.sendLogEntries([]*LogEntry{entry})
}

// sendLogEntries sends multiple log entries to Elasticsearch
func (el *ElasticLogger) sendLogEntries(entries []*LogEntry) error {
	if len(entries) == 0 {
		return nil
	}

	data, err := json.Marshal(entries)
	if err != nil {
		el.stats.mu.Lock()
		el.stats.FailedLogs += int64(len(entries))
		el.stats.mu.Unlock()
		return fmt.Errorf("failed to marshal log entries: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= el.config.MaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(el.config.RetryDelay * time.Duration(attempt))
			el.stats.mu.Lock()
			el.stats.RetryCount++
			el.stats.mu.Unlock()
		}

		req, err := http.NewRequestWithContext(el.ctx, "POST", el.config.URL, bytes.NewReader(data))
		if err != nil {
			lastErr = fmt.Errorf("failed to create request: %w", err)
			continue
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "ElasticLogger/1.0")

		resp, err := el.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("failed to send request: %w", err)
			continue
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			el.stats.mu.Lock()
			el.stats.SuccessLogs += int64(len(entries))
			el.stats.mu.Unlock()
			return nil
		}

		lastErr = fmt.Errorf("elasticsearch returned status %d: %s", resp.StatusCode, string(body))
		
		// Don't retry on client errors (4xx)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			break
		}
	}

	el.stats.mu.Lock()
	el.stats.FailedLogs += int64(len(entries))
	el.stats.mu.Unlock()

	return fmt.Errorf("failed to send log entries after %d attempts: %w", el.config.MaxRetries+1, lastErr)
}

// Flush forces all pending logs to be sent immediately
func (el *ElasticLogger) Flush() error {
	if !el.config.EnableAsync {
		return nil
	}

	el.mu.Lock()
	defer el.mu.Unlock()

	if len(el.batch) > 0 {
		batch := make([]*LogEntry, len(el.batch))
		copy(batch, el.batch)
		el.batch = el.batch[:0]
		return el.sendLogEntries(batch)
	}

	return nil
}

// Close gracefully shuts down the logger
func (el *ElasticLogger) Close() error {
	el.closeMu.Lock()
	if el.closed {
		el.closeMu.Unlock()
		return nil
	}
	el.closed = true
	el.closeMu.Unlock()

	if el.config.EnableAsync {
		el.cancel()
		el.wg.Wait()
	}

	// Flush any remaining logs
	return el.Flush()
}

// GetStats returns current logger statistics
func (el *ElasticLogger) GetStats() LoggerStats {
	return el.stats.GetStats()
}

// GetErrors returns a channel for monitoring errors
func (el *ElasticLogger) GetErrors() <-chan error {
	return el.errorsCh
}

// LogError logs an error with stack trace information
func (el *ElasticLogger) LogError(err error, component, action string, data interface{}) error {
	// Get stack trace
	_, file, line, _ := runtime.Caller(1)
	
	errorData := map[string]interface{}{
		"error":      err.Error(),
		"stack_file": file,
		"stack_line": line,
		"data":       data,
	}

	return el.Log(LogOptions{
		Component: component,
		Action:    action,
		Message:   err.Error(),
		Data:      errorData,
		Level:     ERROR,
	})
}

// Example usage and factory functions
func NewProductionLogger(elasticURL string) (*ElasticLogger, error) {
	config := &Config{
		URL:           elasticURL,
		MaxRetries:    5,
		RetryDelay:    2 * time.Second,
		BatchSize:     200,
		FlushInterval: 5 * time.Second,
		HTTPTimeout:   60 * time.Second,
		MaxQueueSize:  50000,
		EnableAsync:   true,
		ComponentName: "production",
		BufferSize:    5000,
	}
	return NewElasticLogger(config)
}

func NewDevelopmentLogger(elasticURL string) (*ElasticLogger, error) {
	config := &Config{
		URL:           elasticURL,
		MaxRetries:    1,
		RetryDelay:    500 * time.Millisecond,
		BatchSize:     10,
		FlushInterval: 1 * time.Second,
		HTTPTimeout:   10 * time.Second,
		MaxQueueSize:  1000,
		EnableAsync:   false, // Synchronous for development
		ComponentName: "development",
		BufferSize:    100,
	}
	return NewElasticLogger(config)
}