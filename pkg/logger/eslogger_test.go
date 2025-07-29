package logger

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockElasticsearchServer создает mock сервер для тестирования
type MockElasticsearchServer struct {
	server       *httptest.Server
	requests     [][]LogEntry
	requestCount int64
	mutex        sync.RWMutex
	responses    map[int]int // request number -> status code
	delays       map[int]time.Duration // request number -> delay
	shouldFail   bool
}

func NewMockElasticsearchServer() *MockElasticsearchServer {
	mock := &MockElasticsearchServer{
		requests:  make([][]LogEntry, 0),
		responses: make(map[int]int),
		delays:    make(map[int]time.Duration),
	}
	
	mock.server = httptest.NewServer(http.HandlerFunc(mock.handler))
	return mock
}

func (m *MockElasticsearchServer) handler(w http.ResponseWriter, r *http.Request) {
	reqNum := int(atomic.AddInt64(&m.requestCount, 1))
	
	// Применяем задержку если настроена
	if delay, exists := m.delays[reqNum]; exists {
		time.Sleep(delay)
	}
	
	// Проверяем нужно ли вернуть ошибку
	if statusCode, exists := m.responses[reqNum]; exists {
		w.WriteHeader(statusCode)
		if statusCode >= 400 {
			w.Write([]byte(fmt.Sprintf(`{"error": "mock error %d"}`, statusCode)))
			return
		}
	}
	
	if m.shouldFail {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "mock server failure"}`))
		return
	}
	
	// Парсим тело запроса
	var entries []LogEntry
	if err := json.NewDecoder(r.Body).Decode(&entries); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid JSON"}`))
		return
	}
	
	// Сохраняем запрос
	m.mutex.Lock()
	m.requests = append(m.requests, entries)
	m.mutex.Unlock()
	
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"acknowledged": true}`))
}

func (m *MockElasticsearchServer) GetURL() string {
	return m.server.URL
}

func (m *MockElasticsearchServer) Close() {
	m.server.Close()
}

func (m *MockElasticsearchServer) GetRequests() [][]LogEntry {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	
	result := make([][]LogEntry, len(m.requests))
	copy(result, m.requests)
	return result
}

func (m *MockElasticsearchServer) GetRequestCount() int64 {
	return atomic.LoadInt64(&m.requestCount)
}

func (m *MockElasticsearchServer) SetResponseForRequest(reqNum int, statusCode int) {
	m.responses[reqNum] = statusCode
}

func (m *MockElasticsearchServer) SetDelayForRequest(reqNum int, delay time.Duration) {
	m.delays[reqNum] = delay
}

func (m *MockElasticsearchServer) SetShouldFail(fail bool) {
	m.shouldFail = fail
}

func (m *MockElasticsearchServer) Reset() {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	
	m.requests = make([][]LogEntry, 0)
	atomic.StoreInt64(&m.requestCount, 0)
	m.responses = make(map[int]int)
	m.delays = make(map[int]time.Duration)
	m.shouldFail = false
}

// Тесты конфигурации
func TestDefaultConfig(t *testing.T) {
	url := "http://localhost:9200"
	config := DefaultConfig(url)
	
	if config.URL != url {
		t.Errorf("Expected URL %s, got %s", url, config.URL)
	}
	
	if config.MaxRetries != 3 {
		t.Errorf("Expected MaxRetries 3, got %d", config.MaxRetries)
	}
	
	if config.BatchSize != 100 {
		t.Errorf("Expected BatchSize 100, got %d", config.BatchSize)
	}
	
	if !config.EnableAsync {
		t.Error("Expected EnableAsync to be true")
	}
}

// Тесты создания логгера
func TestNewElasticLogger(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		expectError bool
	}{
		{
			name:        "nil config",
			config:      nil,
			expectError: true,
		},
		{
			name: "empty URL",
			config: &Config{
				URL: "",
			},
			expectError: true,
		},
		{
			name: "valid config",
			config: &Config{
				URL:         "http://localhost:9200",
				EnableAsync: true,
			},
			expectError: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := tt.factoryFunc("http://localhost:9200")
			if err != nil {
				t.Errorf("Failed to create %s: %v", tt.name, err)
				return
			}
			defer logger.Close()
			
			if logger.config.EnableAsync != tt.expectedAsync {
				t.Errorf("Expected EnableAsync to be %v for %s, got %v", tt.expectedAsync, tt.name, logger.config.EnableAsync)
			}
		})
	}
}

// Тесты шардирования
func TestSharding(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     1,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    100,
		WorkerPoolSize: 3, // 3 шарда
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Проверяем что создалось правильное количество шардов
	if len(logger.shards) != 3 {
		t.Errorf("Expected 3 shards, got %d", len(logger.shards))
	}
	
	// Тестируем распределение по шардам
	shardUsage := make(map[int]int)
	
	// Отправляем логи с разными component/userID для разного хеширования
	testCases := []struct {
		component string
		userID    string
	}{
		{"comp1", "user1"},
		{"comp2", "user2"},
		{"comp3", "user3"},
		{"comp1", "user4"}, // Тот же компонент, другой пользователь
		{"comp4", "user1"}, // Другой компонент, тот же пользователь
	}
	
	for _, tc := range testCases {
		err = logger.Log(LogOptions{
			Component: tc.component,
			UserID:    tc.userID,
			Action:    "test",
			Message:   "test message",
			Level:     INFO,
		})
		if err != nil {
			t.Errorf("Failed to log: %v", err)
		}
		
		// Определяем какой шард должен был быть выбран
		entry := &LogEntry{Component: tc.component, UserID: tc.userID}
		shard := logger.selectShard(entry)
		shardUsage[shard.id]++
	}
	
	// Проверяем что использовались разные шарды
	usedShards := len(shardUsage)
	if usedShards < 2 {
		t.Errorf("Expected at least 2 shards to be used, got %d", usedShards)
	}
	
	time.Sleep(200 * time.Millisecond)
	
	// Проверяем статистику шардов
	shardStats := logger.GetShardStats()
	if len(shardStats) != 3 {
		t.Errorf("Expected stats for 3 shards, got %d", len(shardStats))
	}
	
	for _, stat := range shardStats {
		if stat["shard_id"] == nil {
			t.Error("Expected shard_id in stats")
		}
		if stat["queue_length"] == nil {
			t.Error("Expected queue_length in stats")
		}
	}
}

// Тесты мониторинга ошибок
func TestErrorMonitoring(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	// Настраиваем сервер на постоянные ошибки
	mock.SetShouldFail(true)
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     1,
		MaxRetries:    1,
		RetryDelay:    10 * time.Millisecond,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    10,
		WorkerPoolSize: 1,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Получаем канал ошибок
	errorsCh := logger.GetErrors()
	
	// Отправляем лог который должен завершиться ошибкой
	err = logger.Info("test-component", "test-action", "test message", nil)
	if err != nil {
		t.Errorf("Failed to log: %v", err)
	}
	
	// Ждем ошибку
	select {
	case logErr := <-errorsCh:
		if logErr == nil {
			t.Error("Expected non-nil error from error channel")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("Expected to receive error from error channel")
	}
	
	stats := logger.GetStats()
	if stats.FailedLogs == 0 {
		t.Error("Expected at least one failed log")
	}
}

// Тесты таймаутов HTTP
func TestHTTPTimeout(t *testing.T) {
	// Создаем сервер с задержкой
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond) // Задержка больше таймаута
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	
	config := &Config{
		URL:         server.URL,
		EnableAsync: false,
		HTTPTimeout: 50 * time.Millisecond, // Короткий таймаут
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	err = logger.Info("test-component", "test-action", "test message", nil)
	if err == nil {
		t.Error("Expected timeout error")
	}
	
	stats := logger.GetStats()
	if stats.FailedLogs != 1 {
		t.Errorf("Expected 1 failed log due to timeout, got %d", stats.FailedLogs)
	}
}

// Тесты валидации конфигурации
func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name           string
		config         *Config
		expectedBatchSize int
		expectedMaxRetries int
	}{
		{
			name: "negative batch size",
			config: &Config{
				URL:       "http://localhost:9200",
				BatchSize: -10,
			},
			expectedBatchSize: 100, // default
		},
		{
			name: "zero batch size",
			config: &Config{
				URL:       "http://localhost:9200",
				BatchSize: 0,
			},
			expectedBatchSize: 100, // default
		},
		{
			name: "negative max retries",
			config: &Config{
				URL:        "http://localhost:9200",
				MaxRetries: -5,
			},
			expectedMaxRetries: 3, // default
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := NewElasticLogger(tt.config)
			if err != nil {
				t.Fatalf("Failed to create logger: %v", err)
			}
			defer logger.Close()
			
			if tt.expectedBatchSize > 0 && logger.config.BatchSize != tt.expectedBatchSize {
				t.Errorf("Expected batch size %d, got %d", tt.expectedBatchSize, logger.config.BatchSize)
			}
			
			if tt.expectedMaxRetries > 0 && logger.config.MaxRetries != tt.expectedMaxRetries {
				t.Errorf("Expected max retries %d, got %d", tt.expectedMaxRetries, logger.config.MaxRetries)
			}
		})
	}
}

// Тесты производительности с большим количеством логов
func TestHighVolumeLogging(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping high volume test in short mode")
	}
	
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:                mock.GetURL(),
		EnableAsync:        true,
		BatchSize:          100,
		FlushInterval:      100 * time.Millisecond,
		BufferSize:         5000,
		WorkerPoolSize:     8,
		MaxConcurrentSends: 20,
		HTTPTimeout:        30 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	const totalLogs = 10000
	start := time.Now()
	
	// Отправляем большое количество логов
	for i := 0; i < totalLogs; i++ {
		err = logger.Info(
			fmt.Sprintf("component-%d", i%10),
			"high-volume-test",
			fmt.Sprintf("High volume message %d", i),
			map[string]interface{}{
				"index":     i,
				"timestamp": time.Now().Unix(),
				"batch":     i / 1000,
			},
		)
		if err != nil {
			t.Errorf("Failed to log message %d: %v", i, err)
		}
		
		// Небольшая пауза каждые 1000 сообщений
		if i%1000 == 0 && i > 0 {
			time.Sleep(10 * time.Millisecond)
		}
	}
	
	// Принудительно сбрасываем буферы
	logger.Flush()
	
	// Ждем обработки всех логов
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-timeout:
			t.Error("Timeout waiting for all logs to be processed")
			goto checkStats
		case <-ticker.C:
			stats := logger.GetStats()
			processed := stats.SuccessLogs + stats.FailedLogs
			if processed >= int64(totalLogs)-stats.DroppedLogs {
				goto checkStats
			}
		}
	}
	
checkStats:
	duration := time.Since(start)
	stats := logger.GetStats()
	
	t.Logf("High volume test completed in %v", duration)
	t.Logf("Total logs: %d", stats.TotalLogs)
	t.Logf("Success logs: %d", stats.SuccessLogs)
	t.Logf("Failed logs: %d", stats.FailedLogs)
	t.Logf("Dropped logs: %d", stats.DroppedLogs)
	t.Logf("Retry count: %d", stats.RetryCount)
	t.Logf("Throughput: %.2f logs/sec", float64(stats.TotalLogs)/duration.Seconds())
	
	if stats.TotalLogs != totalLogs {
		t.Errorf("Expected %d total logs, got %d", totalLogs, stats.TotalLogs)
	}
	
	// Проверяем что большинство логов обработано успешно
	successRate := float64(stats.SuccessLogs) / float64(stats.TotalLogs-stats.DroppedLogs)
	if successRate < 0.95 { // Ожидаем минимум 95% успешных отправок
		t.Errorf("Success rate too low: %.2f%%, expected at least 95%%", successRate*100)
	}
}

// Тесты корректности JSON сериализации
func TestJSONSerialization(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: false,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	complexData := map[string]interface{}{
		"string":  "test string",
		"number":  42,
		"float":   3.14,
		"bool":    true,
		"array":   []string{"a", "b", "c"},
		"nested": map[string]interface{}{
			"key1": "value1",
			"key2": 123,
		},
		"null": nil,
	}
	
	err = logger.Log(LogOptions{
		Component:     "json-test",
		Action:        "serialize",
		Message:       "Testing JSON serialization",
		CustomMessage: "Custom message with unicode: 🚀",
		UserID:        "user-123",
		Type:          "test",
		Data:          complexData,
		Level:         INFO,
	})
	
	if err != nil {
		t.Errorf("Failed to log complex data: %v", err)
	}
	
	time.Sleep(50 * time.Millisecond)
	requests := mock.GetRequests()
	
	if len(requests) != 1 || len(requests[0]) != 1 {
		t.Fatalf("Expected exactly one log entry")
	}
	
	entry := requests[0][0]
	
	// Проверяем что все поля корректно сериализовались
	if entry.CustomMessage != "Custom message with unicode: 🚀" {
		t.Errorf("Unicode not preserved in CustomMessage")
	}
	
	data, ok := entry.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}
	
	if data["string"] != "test string" {
		t.Error("String field not preserved")
	}
	
	// JSON числа десериализуются как float64
	if data["number"].(float64) != 42 {
		t.Error("Number field not preserved")
	}
	
	if data["bool"] != true {
		t.Error("Bool field not preserved")
	}
	
	nested, ok := data["nested"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected nested to be a map")
	}
	
	if nested["key1"] != "value1" {
		t.Error("Nested string not preserved")
	}
}

// Example тесты для документации
func ExampleElasticLogger_Info() {
	// Создаем логгер для разработки
	logger, err := NewDevelopmentLogger("http://localhost:9200")
	if err != nil {
		panic(err)
	}
	defer logger.Close()
	
	// Логируем информационное сообщение
	err = logger.Info("user-service", "login", "User logged in successfully", map[string]string{
		"user_id": "12345",
		"ip":      "192.168.1.1",
	})
	if err != nil {
		panic(err)
	}
}

func ExampleElasticLogger_Log() {
	logger, err := NewProductionLogger("http://localhost:9200")
	if err != nil {
		panic(err)
	}
	defer logger.Close()
	
	// Логируем с полными опциями
	err = logger.Log(LogOptions{
		Component:     "payment-service",
		Action:        "process_payment",
		Code:          "PAY001",
		Message:       "Payment processed successfully",
		CustomMessage: "Credit card payment completed",
		UserID:        "user-789",
		Type:          "payment",
		Data: map[string]interface{}{
			"amount":   99.99,
			"currency": "USD",
			"method":   "credit_card",
		},
		Level: INFO,
	})
	if err != nil {
		panic(err)
	}
}

func ExampleElasticLogger_GetStats() {
	logger, err := NewHighLoadLogger("http://localhost:9200")
	if err != nil {
		panic(err)
	}
	defer logger.Close()
	
	// Отправляем несколько логов
	for i := 0; i < 100; i++ {
		logger.Info("test", "example", fmt.Sprintf("Message %d", i), nil)
	}
	
	// Получаем статистику
	stats := logger.GetStats()
	fmt.Printf("Total: %d, Success: %d, Failed: %d, Dropped: %d\n",
		stats.TotalLogs, stats.SuccessLogs, stats.FailedLogs, stats.DroppedLogs)
}
			logger, err := NewElasticLogger(tt.config)
			
			if tt.expectError {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				if logger != nil {
					t.Error("Expected nil logger on error")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if logger == nil {
					t.Error("Expected logger, got nil")
				} else {
					logger.Close()
				}
			}
		})
	}
}

// Тесты логирования в синхронном режиме
func TestSynchronousLogging(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: false,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Тестируем базовое логирование
	err = logger.Info("test-component", "test-action", "test message", map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("Failed to log: %v", err)
	}
	
	// Проверяем что запрос был отправлен
	time.Sleep(100 * time.Millisecond)
	requests := mock.GetRequests()
	if len(requests) != 1 {
		t.Errorf("Expected 1 request, got %d", len(requests))
	}
	
	if len(requests[0]) != 1 {
		t.Errorf("Expected 1 log entry, got %d", len(requests[0]))
	}
	
	entry := requests[0][0]
	if entry.Component != "test-component" {
		t.Errorf("Expected component 'test-component', got '%s'", entry.Component)
	}
	
	if entry.Action != "test-action" {
		t.Errorf("Expected action 'test-action', got '%s'", entry.Action)
	}
	
	if entry.Message != "test message" {
		t.Errorf("Expected message 'test message', got '%s'", entry.Message)
	}
}

// Тесты асинхронного логирования
func TestAsynchronousLogging(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     3,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    100,
		WorkerPoolSize: 2,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Отправляем несколько логов
	for i := 0; i < 5; i++ {
		err = logger.Info("test-component", "test-action", fmt.Sprintf("message %d", i), map[string]int{"index": i})
		if err != nil {
			t.Errorf("Failed to log message %d: %v", i, err)
		}
	}
	
	// Принудительно сбрасываем буферы
	logger.Flush()
	
	// Ждем обработки
	time.Sleep(200 * time.Millisecond)
	
	// Проверяем статистику
	stats := logger.GetStats()
	if stats.TotalLogs != 5 {
		t.Errorf("Expected 5 total logs, got %d", stats.TotalLogs)
	}
	
	if stats.SuccessLogs != 5 {
		t.Errorf("Expected 5 successful logs, got %d", stats.SuccessLogs)
	}
	
	// Проверяем что запросы были отправлены
	requests := mock.GetRequests()
	totalEntries := 0
	for _, req := range requests {
		totalEntries += len(req)
	}
	
	if totalEntries != 5 {
		t.Errorf("Expected 5 total entries, got %d", totalEntries)
	}
}

// Тесты батчирования
func TestBatching(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     3,
		FlushInterval: 1 * time.Second, // Большой интервал, чтобы батчи срабатывали по размеру
		BufferSize:    100,
		WorkerPoolSize: 1, // Один воркер для предсказуемости
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Отправляем точно 6 логов (2 батча по 3)
	for i := 0; i < 6; i++ {
		err = logger.Info("test-component", "test-action", fmt.Sprintf("message %d", i), nil)
		if err != nil {
			t.Errorf("Failed to log message %d: %v", i, err)
		}
	}
	
	// Ждем обработки батчей
	time.Sleep(200 * time.Millisecond)
	
	requests := mock.GetRequests()
	if len(requests) != 2 {
		t.Errorf("Expected 2 batch requests, got %d", len(requests))
	}
	
	for i, req := range requests {
		if len(req) != 3 {
			t.Errorf("Expected batch %d to have 3 entries, got %d", i, len(req))
		}
	}
}

// Тесты обработки ошибок и повторов
func TestRetryMechanism(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	// Настраиваем сервер: первый запрос - ошибка, второй - успех
	mock.SetResponseForRequest(1, http.StatusInternalServerError)
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     1,
		MaxRetries:    2,
		RetryDelay:    50 * time.Millisecond,
		FlushInterval: 10 * time.Millisecond,
		BufferSize:    10,
		WorkerPoolSize: 1,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Отправляем один лог
	err = logger.Info("test-component", "test-action", "test message", nil)
	if err != nil {
		t.Errorf("Failed to log: %v", err)
	}
	
	// Ждем обработки с повторами
	time.Sleep(300 * time.Millisecond)
	
	stats := logger.GetStats()
	if stats.RetryCount == 0 {
		t.Error("Expected at least one retry")
	}
	
	if stats.SuccessLogs != 1 {
		t.Errorf("Expected 1 successful log after retry, got %d", stats.SuccessLogs)
	}
}

// Тесты обработки переполнения очереди
func TestQueueOverflow(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	// Настраиваем задержки для замедления обработки
	for i := 1; i <= 10; i++ {
		mock.SetDelayForRequest(i, 100*time.Millisecond)
	}
	
	config := &Config{
		URL:                mock.GetURL(),
		EnableAsync:        true,
		BatchSize:          1,
		FlushInterval:      10 * time.Millisecond,
		BufferSize:         5, // Маленький буфер
		WorkerPoolSize:     1,
		MaxConcurrentSends: 1,
		HTTPTimeout:        5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Быстро отправляем много логов
	for i := 0; i < 20; i++ {
		err = logger.Info("test-component", "test-action", fmt.Sprintf("message %d", i), nil)
		// Некоторые логи должны быть отброшены из-за переполнения
	}
	
	time.Sleep(500 * time.Millisecond)
	
	stats := logger.GetStats()
	if stats.DroppedLogs == 0 {
		t.Error("Expected some logs to be dropped due to queue overflow")
	}
	
	if stats.TotalLogs != stats.SuccessLogs+stats.FailedLogs+stats.DroppedLogs {
		t.Error("Statistics don't add up correctly")
	}
}

// Тесты различных уровней логирования
func TestLogLevels(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: false,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	tests := []struct {
		name     string
		logFunc  func() error
		expected string
	}{
		{
			name:     "Debug",
			logFunc:  func() error { return logger.Debug("comp", "act", "debug msg", nil) },
			expected: "DEBUG",
		},
		{
			name:     "Info",
			logFunc:  func() error { return logger.Info("comp", "act", "info msg", nil) },
			expected: "INFO",
		},
		{
			name:     "Warn",
			logFunc:  func() error { return logger.Warn("comp", "act", "warn msg", nil) },
			expected: "WARN",
		},
		{
			name:     "Error",
			logFunc:  func() error { return logger.Error("comp", "act", "error msg", nil) },
			expected: "ERROR",
		},
		{
			name:     "Fatal",
			logFunc:  func() error { return logger.Fatal("comp", "act", "fatal msg", nil) },
			expected: "FATAL",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock.Reset()
			
			err := tt.logFunc()
			if err != nil {
				t.Errorf("Failed to log %s: %v", tt.name, err)
			}
			
			time.Sleep(50 * time.Millisecond)
			requests := mock.GetRequests()
			
			if len(requests) != 1 || len(requests[0]) != 1 {
				t.Fatalf("Expected exactly one log entry")
			}
			
			entry := requests[0][0]
			if entry.Type != tt.expected {
				t.Errorf("Expected type %s, got %s", tt.expected, entry.Type)
			}
		})
	}
}

// Тесты закрытия логгера
func TestLoggerClose(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     10,
		FlushInterval: 100 * time.Millisecond,
		BufferSize:    100,
		WorkerPoolSize: 2,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	
	// Отправляем несколько логов
	for i := 0; i < 5; i++ {
		logger.Info("comp", "act", fmt.Sprintf("msg %d", i), nil)
	}
	
	// Закрываем логгер
	err = logger.Close()
	if err != nil {
		t.Errorf("Failed to close logger: %v", err)
	}
	
	// Повторное закрытие должно быть безопасным
	err = logger.Close()
	if err != nil {
		t.Errorf("Second close should not return error: %v", err)
	}
	
	// Попытка логирования после закрытия должна вернуть ошибку
	err = logger.Info("comp", "act", "msg after close", nil)
	if err == nil {
		t.Error("Expected error when logging after close")
	}
}

// Тесты с контекстом
func TestLogWithContext(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: true,
		BatchSize:   1,
		BufferSize:  10,
		WorkerPoolSize: 1,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Тест с отменённым контекстом
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Отменяем сразу
	
	err = logger.LogWithContext(ctx, LogOptions{
		Component: "test",
		Action:    "test",
		Message:   "test",
		Level:     INFO,
	})
	
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
	
	// Тест с таймаутом контекста
	ctx, cancel = context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	
	// Заполняем очередь чтобы создать задержку
	for i := 0; i < 20; i++ {
		logger.Info("comp", "act", fmt.Sprintf("msg %d", i), nil)
	}
	
	time.Sleep(10 * time.Millisecond) // Ждем пока контекст истечет
	
	err = logger.LogWithContext(ctx, LogOptions{
		Component: "test",
		Action:    "test",
		Message:   "test with timeout",
		Level:     INFO,
	})
	
	// Ошибка может быть или не быть в зависимости от timing
	// Главное что приложение не падает
}

// Тесты статистики
func TestStatistics(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     2,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    10,
		WorkerPoolSize: 1,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	// Отправляем несколько успешных логов
	for i := 0; i < 4; i++ {
		err = logger.Info("comp", "act", fmt.Sprintf("msg %d", i), nil)
		if err != nil {
			t.Errorf("Failed to log message %d: %v", i, err)
		}
	}
	
	// Ждем обработки
	time.Sleep(200 * time.Millisecond)
	
	stats := logger.GetStats()
	
	if stats.TotalLogs != 4 {
		t.Errorf("Expected 4 total logs, got %d", stats.TotalLogs)
	}
	
	if stats.SuccessLogs != 4 {
		t.Errorf("Expected 4 successful logs, got %d", stats.SuccessLogs)
	}
	
	if stats.FailedLogs != 0 {
		t.Errorf("Expected 0 failed logs, got %d", stats.FailedLogs)
	}
	
	if stats.DroppedLogs != 0 {
		t.Errorf("Expected 0 dropped logs, got %d", stats.DroppedLogs)
	}
}

// Тесты логирования ошибок со стек-трейсом
func TestLogError(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: false,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	testErr := fmt.Errorf("test error")
	err = logger.LogError(testErr, "test-component", "test-action", map[string]string{"key": "value"})
	if err != nil {
		t.Errorf("Failed to log error: %v", err)
	}
	
	time.Sleep(50 * time.Millisecond)
	requests := mock.GetRequests()
	
	if len(requests) != 1 || len(requests[0]) != 1 {
		t.Fatalf("Expected exactly one log entry")
	}
	
	entry := requests[0][0]
	if entry.Component != "test-component" {
		t.Errorf("Expected component 'test-component', got '%s'", entry.Component)
	}
	
	if entry.Message != "test error" {
		t.Errorf("Expected message 'test error', got '%s'", entry.Message)
	}
	
	// Проверяем что в данных есть информация о стеке
	data, ok := entry.Data.(map[string]interface{})
	if !ok {
		t.Fatal("Expected data to be a map")
	}
	
	if data["error"] != "test error" {
		t.Errorf("Expected error field to be 'test error', got '%v'", data["error"])
	}
	
	if data["stack_file"] == nil {
		t.Error("Expected stack_file to be present")
	}
	
	if data["stack_line"] == nil {
		t.Error("Expected stack_line to be present")
	}
}

// Бенчмарки
func BenchmarkSyncLogging(b *testing.B) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:         mock.GetURL(),
		EnableAsync: false,
		HTTPTimeout: 5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		logger.Info("bench-component", "bench-action", "bench message", map[string]int{"iteration": i})
	}
}

func BenchmarkAsyncLogging(b *testing.B) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:           mock.GetURL(),
		EnableAsync:   true,
		BatchSize:     100,
		FlushInterval: 1 * time.Second,
		BufferSize:    10000,
		WorkerPoolSize: 4,
		HTTPTimeout:   5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		b.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		logger.Info("bench-component", "bench-action", "bench message", map[string]int{"iteration": i})
	}
	
	logger.Flush()
}

// Stress тесты
func TestConcurrentLogging(t *testing.T) {
	mock := NewMockElasticsearchServer()
	defer mock.Close()
	
	config := &Config{
		URL:                mock.GetURL(),
		EnableAsync:        true,
		BatchSize:          50,
		FlushInterval:      100 * time.Millisecond,
		BufferSize:         1000,
		WorkerPoolSize:     4,
		MaxConcurrentSends: 10,
		HTTPTimeout:        5 * time.Second,
	}
	
	logger, err := NewElasticLogger(config)
	if err != nil {
		t.Fatalf("Failed to create logger: %v", err)
	}
	defer logger.Close()
	
	const numGoroutines = 10
	const logsPerGoroutine = 100
	
	var wg sync.WaitGroup
	wg.Add(numGoroutines)
	
	for i := 0; i < numGoroutines; i++ {
		go func(goroutineID int) {
			defer wg.Done()
			
			for j := 0; j < logsPerGoroutine; j++ {
				err := logger.Info(
					fmt.Sprintf("goroutine-%d", goroutineID),
					"concurrent-test",
					fmt.Sprintf("message %d from goroutine %d", j, goroutineID),
					map[string]int{"goroutine": goroutineID, "iteration": j},
				)
				if err != nil {
					t.Errorf("Failed to log from goroutine %d, iteration %d: %v", goroutineID, j, err)
				}
			}
		}(i)
	}
	
	wg.Wait()
	
	// Принудительно сбрасываем все буферы и ждем обработки
	logger.Flush()
	time.Sleep(500 * time.Millisecond)
	
	stats := logger.GetStats()
	expectedTotal := int64(numGoroutines * logsPerGoroutine)
	
	if stats.TotalLogs != expectedTotal {
		t.Errorf("Expected %d total logs, got %d", expectedTotal, stats.TotalLogs)
	}
	
	// Проверяем что все логи обработаны (успешно или с ошибкой)
	processed := stats.SuccessLogs + stats.FailedLogs
	if processed < expectedTotal-stats.DroppedLogs {
		t.Errorf("Not all logs were processed. Expected at least %d, got %d", expectedTotal-stats.DroppedLogs, processed)
	}
}

// Тесты factory функций
func TestFactoryFunctions(t *testing.T) {
	tests := []struct {
		name        string
		factoryFunc func(string) (*ElasticLogger, error)
		expectedAsync bool
	}{
		{
			name:        "Production Logger",
			factoryFunc: NewProductionLogger,
			expectedAsync: true,
		},
		{
			name:        "Development Logger",
			factoryFunc: NewDevelopmentLogger,
			expectedAsync: false,
		},
		{
			name:        "High Load Logger",
			factoryFunc: NewHighLoadLogger,
			expectedAsync: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {