package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
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
	URL                string        // Elasticsearch endpoint URL
	MaxRetries         int           // Maximum number of retry attempts
	RetryDelay         time.Duration // Delay between retries
	BatchSize          int           // Size of batch for bulk operations
	FlushInterval      time.Duration // Interval to flush logs
	HTTPTimeout        time.Duration // HTTP client timeout
	MaxQueueSize       int           // Maximum queue size before dropping logs
	EnableAsync        bool          // Enable asynchronous logging
	ComponentName      string        // Default component name
	BufferSize         int           // Buffer size for async channel
	MaxConcurrentSends int           // Maximum concurrent HTTP requests
	WorkerPoolSize     int           // Number of worker goroutines (shards)
}

// DefaultConfig returns a default configuration
func DefaultConfig(elasticURL string) *Config {
	return &Config{
		URL:                elasticURL,
		MaxRetries:         3,
		RetryDelay:         time.Second,
		BatchSize:          100,
		FlushInterval:      10 * time.Second,
		HTTPTimeout:        30 * time.Second,
		MaxQueueSize:       10000,
		EnableAsync:        true,
		ComponentName:      "default",
		BufferSize:         1000,
		MaxConcurrentSends: 10,
		WorkerPoolSize:     5,
	}
}

// LoggerStats holds statistics about the logger using atomic operations
type LoggerStats struct {
	TotalLogs    int64
	SuccessLogs  int64
	FailedLogs   int64
	DroppedLogs  int64
	RetryCount   int64
	ActiveSends  int64
}

// GetStats returns a snapshot of current logger statistics
func (s *LoggerStats) GetStats() LoggerStats {
	return LoggerStats{
		TotalLogs:    atomic.LoadInt64(&s.TotalLogs),
		SuccessLogs:  atomic.LoadInt64(&s.SuccessLogs),
		FailedLogs:   atomic.LoadInt64(&s.FailedLogs),
		DroppedLogs:  atomic.LoadInt64(&s.DroppedLogs),
		RetryCount:   atomic.LoadInt64(&s.RetryCount),
		ActiveSends:  atomic.LoadInt64(&s.ActiveSends),
	}
}

// batchJob represents a job to send a batch of log entries
type batchJob struct {
	entries []*LogEntry
	retries int
}

// workerShard represents a single shard with its own batch and processing
type workerShard struct {
	id           int
	queue        chan *LogEntry
	batchJobs    chan *batchJob
	currentBatch []*LogEntry
	ticker       *time.Ticker
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	config       *Config
	client       *http.Client
	stats        *LoggerStats
	semaphore    chan struct{}
	errorsCh     chan error
}

// newWorkerShard creates a new worker shard
func newWorkerShard(id int, config *Config, client *http.Client, stats *LoggerStats, semaphore chan struct{}, errorsCh chan error) *workerShard {
	ctx, cancel := context.WithCancel(context.Background())
	
	// Рассчитываем размер буфера для каждого воркера
	workerBufferSize := config.BufferSize / config.WorkerPoolSize
	if workerBufferSize < 10 {
		workerBufferSize = 10
	}
	
	shard := &workerShard{
		id:           id,
		queue:        make(chan *LogEntry, workerBufferSize),
		batchJobs:    make(chan *batchJob, workerBufferSize/config.BatchSize+1),
		currentBatch: make([]*LogEntry, 0, config.BatchSize),
		ticker:       time.NewTicker(config.FlushInterval),
		ctx:          ctx,
		cancel:       cancel,
		config:       config,
		client:       client,
		stats:        stats,
		semaphore:    semaphore,
		errorsCh:     errorsCh,
	}
	
	// Запускаем горутины для этого шарда
	shard.wg.Add(2)
	go shard.batchProcessor()
	go shard.sender()
	
	return shard
}

// batchProcessor обрабатывает входящие логи и формирует батчи
func (ws *workerShard) batchProcessor() {
	defer ws.wg.Done()
	defer ws.ticker.Stop()
	
	for {
		select {
		case <-ws.ctx.Done():
			// Отправляем оставшиеся логи
			ws.flushCurrentBatch()
			return
			
		case entry := <-ws.queue:
			ws.currentBatch = append(ws.currentBatch, entry)
			
			if len(ws.currentBatch) >= ws.config.BatchSize {
				ws.flushCurrentBatch()
			}
			
		case <-ws.ticker.C:
			if len(ws.currentBatch) > 0 {
				ws.flushCurrentBatch()
			}
		}
	}
}

// flushCurrentBatch отправляет текущий батч в канал заданий
func (ws *workerShard) flushCurrentBatch() {
	if len(ws.currentBatch) == 0 {
		return
	}
	
	batch := make([]*LogEntry, len(ws.currentBatch))
	copy(batch, ws.currentBatch)
	ws.currentBatch = ws.currentBatch[:0]
	
	job := &batchJob{
		entries: batch,
		retries: 0,
	}
	
	select {
	case ws.batchJobs <- job:
	default:
		// Канал заданий переполнен, отправляем напрямую
		go ws.processBatchJob(job)
	}
}

// sender обрабатывает задания отправки батчей
func (ws *workerShard) sender() {
	defer ws.wg.Done()
	
	for {
		select {
		case <-ws.ctx.Done():
			return
		case job := <-ws.batchJobs:
			ws.processBatchJob(job)
		}
	}
}

// processBatchJob обрабатывает отдельную задачу отправки батча
func (ws *workerShard) processBatchJob(job *batchJob) {
	// Получаем семафор для ограничения concurrent sends
	ws.semaphore <- struct{}{}
	defer func() { <-ws.semaphore }()
	
	atomic.AddInt64(&ws.stats.ActiveSends, 1)
	defer atomic.AddInt64(&ws.stats.ActiveSends, -1)
	
	err := ws.sendLogEntries(job.entries)
	if err != nil {
		// Если это не последняя попытка, отправляем на повтор
		if job.retries < ws.config.MaxRetries {
			job.retries++
			atomic.AddInt64(&ws.stats.RetryCount, 1)
			
			// Задержка перед повтором
			time.Sleep(ws.config.RetryDelay * time.Duration(job.retries))
			
			// Отправляем на повтор
			select {
			case ws.batchJobs <- job:
			default:
				// Канал заданий переполнен, отправляем напрямую
				go ws.processBatchJob(job)
			}
			return
		}
		
		// Отправляем ошибку в канал мониторинга
		select {
		case ws.errorsCh <- err:
		default:
			// Error channel переполнен, игнорируем
		}
	}
}

// sendLogEntries sends multiple log entries to Elasticsearch
func (ws *workerShard) sendLogEntries(entries []*LogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	
	data, err := json.Marshal(entries)
	if err != nil {
		atomic.AddInt64(&ws.stats.FailedLogs, int64(len(entries)))
		return fmt.Errorf("failed to marshal log entries: %w", err)
	}
	
	req, err := http.NewRequestWithContext(ws.ctx, "POST", ws.config.URL, bytes.NewReader(data))
	if err != nil {
		atomic.AddInt64(&ws.stats.FailedLogs, int64(len(entries)))
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ElasticLogger/1.0")
	
	resp, err := ws.client.Do(req)
	if err != nil {
		atomic.AddInt64(&ws.stats.FailedLogs, int64(len(entries)))
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		atomic.AddInt64(&ws.stats.SuccessLogs, int64(len(entries)))
		return nil
	}
	
	body, _ := io.ReadAll(resp.Body)
	atomic.AddInt64(&ws.stats.FailedLogs, int64(len(entries)))
	return fmt.Errorf("elasticsearch returned status %d: %s", resp.StatusCode, string(body))
}

// flush отправляет все накопленные логи немедленно
func (ws *workerShard) flush() {
	if len(ws.currentBatch) > 0 {
		ws.flushCurrentBatch()
	}
}

// close закрывает воркер
func (ws *workerShard) close() {
	ws.cancel()
	ws.wg.Wait()
}

// ElasticLogger represents the main logger instance
type ElasticLogger struct {
	config    *Config
	client    *http.Client
	shards    []*workerShard
	closed    int32
	stats     *LoggerStats
	errorsCh  chan error
	semaphore chan struct{}
}

// NewElasticLogger creates a new instance of ElasticLogger
func NewElasticLogger(config *Config) (*ElasticLogger, error) {
	if config == nil {
		return nil, fmt.Errorf("config cannot be nil")
	}
	
	if config.URL == "" {
		return nil, fmt.Errorf("elasticsearch URL cannot be empty")
	}
	
	// Validate and set defaults
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.MaxRetries < 0 {
		config.MaxRetries = 3
	}
	if config.BufferSize <= 0 {
		config.BufferSize = 1000
	}
	if config.MaxConcurrentSends <= 0 {
		config.MaxConcurrentSends = 10
	}
	if config.WorkerPoolSize <= 0 {
		config.WorkerPoolSize = 5
	}
	
	logger := &ElasticLogger{
		config:    config,
		client:    &http.Client{Timeout: config.HTTPTimeout},
		stats:     &LoggerStats{},
		errorsCh:  make(chan error, 100),
		semaphore: make(chan struct{}, config.MaxConcurrentSends),
	}
	
	if config.EnableAsync {
		// Создаем воркеры (шарды)
		logger.shards = make([]*workerShard, config.WorkerPoolSize)
		for i := 0; i < config.WorkerPoolSize; i++ {
			logger.shards[i] = newWorkerShard(i, config, logger.client, logger.stats, logger.semaphore, logger.errorsCh)
		}
	}
	
	return logger, nil
}

// hashString вычисляет хэш строки для выбора шарда
func (el *ElasticLogger) hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}

// selectShard выбирает шард для записи лога
func (el *ElasticLogger) selectShard(entry *LogEntry) *workerShard {
	// Используем комбинацию Component и UserID для более равномерного распределения
	key := entry.Component + entry.UserID
	if key == "" {
		key = entry.Component
	}
	
	hash := el.hashString(key)
	shardIndex := hash % uint32(len(el.shards))
	return el.shards[shardIndex]
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
	if atomic.LoadInt32(&el.closed) == 1 {
		return fmt.Errorf("logger is closed")
	}
	
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
	
	atomic.AddInt64(&el.stats.TotalLogs, 1)
	
	if el.config.EnableAsync {
		// Выбираем шард для записи
		shard := el.selectShard(entry)
		
		select {
		case shard.queue <- entry:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Queue is full, drop the log
			atomic.AddInt64(&el.stats.DroppedLogs, 1)
			return fmt.Errorf("log queue is full, dropping log entry")
		}
	}
	
	// Synchronous logging
	return el.sendLogEntry(entry)
}

// sendLogEntry sends a single log entry to Elasticsearch (for sync mode)
func (el *ElasticLogger) sendLogEntry(entry *LogEntry) error {
	entries := []*LogEntry{entry}
	
	data, err := json.Marshal(entries)
	if err != nil {
		atomic.AddInt64(&el.stats.FailedLogs, 1)
		return fmt.Errorf("failed to marshal log entries: %w", err)
	}
	
	req, err := http.NewRequest("POST", el.config.URL, bytes.NewReader(data))
	if err != nil {
		atomic.AddInt64(&el.stats.FailedLogs, 1)
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "ElasticLogger/1.0")
	
	resp, err := el.client.Do(req)
	if err != nil {
		atomic.AddInt64(&el.stats.FailedLogs, 1)
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		atomic.AddInt64(&el.stats.SuccessLogs, 1)
		return nil
	}
	
	body, _ := io.ReadAll(resp.Body)
	atomic.AddInt64(&el.stats.FailedLogs, 1)
	return fmt.Errorf("elasticsearch returned status %d: %s", resp.StatusCode, string(body))
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

// Flush forces all pending logs to be sent immediately
func (el *ElasticLogger) Flush() error {
	if !el.config.EnableAsync {
		return nil
	}
	
	// Flush all shards
	for _, shard := range el.shards {
		shard.flush()
	}
	
	return nil
}

// Close gracefully shuts down the logger
func (el *ElasticLogger) Close() error {
	if !atomic.CompareAndSwapInt32(&el.closed, 0, 1) {
		return nil // Already closed
	}
	
	if el.config.EnableAsync {
		// Закрываем все шарды
		for _, shard := range el.shards {
			shard.close()
		}
	}
	
	return nil
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

// GetActiveSends returns the number of currently active HTTP requests
func (el *ElasticLogger) GetActiveSends() int64 {
	return atomic.LoadInt64(&el.stats.ActiveSends)
}

// GetShardStats returns statistics for each shard
func (el *ElasticLogger) GetShardStats() []map[string]interface{} {
	if !el.config.EnableAsync {
		return nil
	}
	
	stats := make([]map[string]interface{}, len(el.shards))
	for i, shard := range el.shards {
		stats[i] = map[string]interface{}{
			"shard_id":      shard.id,
			"queue_length":  len(shard.queue),
			"batch_length":  len(shard.currentBatch),
			"jobs_pending":  len(shard.batchJobs),
		}
	}
	return stats
}

// Factory functions for different environments
func NewProductionLogger(elasticURL string) (*ElasticLogger, error) {
	config := &Config{
		URL:                elasticURL,
		MaxRetries:         5,
		RetryDelay:         2 * time.Second,
		BatchSize:          200,
		FlushInterval:      5 * time.Second,
		HTTPTimeout:        60 * time.Second,
		MaxQueueSize:       50000,
		EnableAsync:        true,
		ComponentName:      "production",
		BufferSize:         5000,
		MaxConcurrentSends: 20,
		WorkerPoolSize:     10,
	}
	return NewElasticLogger(config)
}

func NewDevelopmentLogger(elasticURL string) (*ElasticLogger, error) {
	config := &Config{
		URL:                elasticURL,
		MaxRetries:         1,
		RetryDelay:         500 * time.Millisecond,
		BatchSize:          10,
		FlushInterval:      1 * time.Second,
		HTTPTimeout:        10 * time.Second,
		MaxQueueSize:       1000,
		EnableAsync:        false,
		ComponentName:      "development",
		BufferSize:         100,
		MaxConcurrentSends: 5,
		WorkerPoolSize:     3,
	}
	return NewElasticLogger(config)
}

func NewHighLoadLogger(elasticURL string) (*ElasticLogger, error) {
	config := &Config{
		URL:                elasticURL,
		MaxRetries:         3,
		RetryDelay:         1 * time.Second,
		BatchSize:          500,
		FlushInterval:      2 * time.Second,
		HTTPTimeout:        30 * time.Second,
		MaxQueueSize:       100000,
		EnableAsync:        true,
		ComponentName:      "high-load",
		BufferSize:         10000,
		MaxConcurrentSends: 50,
		WorkerPoolSize:     20,
	}
	return NewElasticLogger(config)
}