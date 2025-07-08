package config

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all configuration for the application
type Config struct {
	Database DatabaseConfig
	Server   ServerConfig
	Elastic  ElasticConfig
	Logger   LoggerConfig
	App      AppConfig
}

// DatabaseConfig holds database configuration
type DatabaseConfig struct {
	Host     string
	User     string
	Password string
	Database string
	Port     int
	SSLMode  string
	TimeZone string
	DSN      string
}

// ServerConfig holds server configuration
type ServerConfig struct {
	Port            int
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	ShutdownTimeout time.Duration
	Host            string
}

// ElasticConfig holds Elasticsearch configuration
type ElasticConfig struct {
	URL      string
	Username string
	Password string
	Index    string
}

// LoggerConfig holds logging configuration
type LoggerConfig struct {
	Level  string
	Format string
}

// AppConfig holds application-specific configuration
type AppConfig struct {
	Environment string
	Name        string
	Version     string
}

// Load loads configuration from environment variables
func Load() (*Config, error) {
	// Load .env file based on environment
	env := os.Getenv("APP_ENV")
	if env == "" {
		env = "dev"
	}

	envFile := fmt.Sprintf("%s.env", env)
	if err := godotenv.Load(envFile); err != nil {
		log.Printf("Warning: Could not load %s file: %v", envFile, err)
	}

	config := &Config{
		Database: DatabaseConfig{
			Host:     getEnv("POSTGRES_HOST", "localhost"),
			User:     getEnv("POSTGRES_USER", "postgres"),
			Password: getEnv("POSTGRES_PASSWORD", ""),
			Database: getEnv("POSTGRES_DB", "api_template"),
			Port:     getEnvAsInt("POSTGRES_PORT", 5432),
			SSLMode:  getEnv("POSTGRES_SSL_MODE", "disable"),
			TimeZone: getEnv("POSTGRES_TIMEZONE", "UTC"),
		},
		Server: ServerConfig{
			Port:            getEnvAsInt("SERVER_PORT", 8080),
			Host:            getEnv("SERVER_HOST", "0.0.0.0"),
			ReadTimeout:     getEnvAsDuration("SERVER_READ_TIMEOUT", 10*time.Second),
			WriteTimeout:    getEnvAsDuration("SERVER_WRITE_TIMEOUT", 10*time.Second),
			ShutdownTimeout: getEnvAsDuration("SERVER_SHUTDOWN_TIMEOUT", 30*time.Second),
		},
		Elastic: ElasticConfig{
			URL:      getEnv("ES_URL", "http://localhost:9200"),
			Username: getEnv("ES_USERNAME", ""),
			Password: getEnv("ES_PASSWORD", ""),
			Index:    getEnv("ES_INDEX", "api_template"),
		},
		Logger: LoggerConfig{
			Level:  getEnv("LOG_LEVEL", "info"),
			Format: getEnv("LOG_FORMAT", "json"),
		},
		App: AppConfig{
			Environment: getEnv("APP_ENV", "dev"),
			Name:        getEnv("APP_NAME", "api-template"),
			Version:     getEnv("APP_VERSION", "1.0.0"),
		},
	}

	// Build database DSN
	config.Database.DSN = fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%d sslmode=%s TimeZone=%s",
		config.Database.Host,
		config.Database.User,
		config.Database.Password,
		config.Database.Database,
		config.Database.Port,
		config.Database.SSLMode,
		config.Database.TimeZone,
	)
	log.Println(config.Database.DSN)

	// Validate required configuration
	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// validate validates the configuration
func (c *Config) validate() error {
	if c.Database.Host == "" {
		return fmt.Errorf("POSTGRES_HOST is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("POSTGRES_USER is required")
	}
	if c.Database.Database == "" {
		return fmt.Errorf("POSTGRES_DB is required")
	}
	if c.Database.Port <= 0 {
		return fmt.Errorf("POSTGRES_PORT must be a positive integer")
	}
	if c.Server.Port <= 0 {
		return fmt.Errorf("SERVER_PORT must be a positive integer")
	}
	if c.Elastic.URL == "" {
		return fmt.Errorf("ES_URL is required")
	}

	// Validate log level
	validLogLevels := []string{"debug", "info", "warn", "error", "fatal", "panic"}
	if !contains(validLogLevels, c.Logger.Level) {
		return fmt.Errorf("LOG_LEVEL must be one of: %v", validLogLevels)
	}

	return nil
}

// IsDevelopment returns true if the application is running in development mode
func (c *Config) IsDevelopment() bool {
	return c.App.Environment == "dev" || c.App.Environment == "development"
}

// IsProduction returns true if the application is running in production mode
func (c *Config) IsProduction() bool {
	return c.App.Environment == "prod" || c.App.Environment == "production"
}

// GetDatabaseURL returns the database URL for connection
func (c *Config) GetDatabaseURL() string {
	return c.Database.DSN
}

// GetServerAddress returns the server address
func (c *Config) GetServerAddress() string {
	return fmt.Sprintf("%s:%d", c.Server.Host, c.Server.Port)
}

// Helper functions

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as integer or returns a default value
func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvAsDuration gets an environment variable as duration or returns a default value
func getEnvAsDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if duration, err := time.ParseDuration(value); err == nil {
			return duration
		}
	}
	return defaultValue
}

// getEnvAsBool gets an environment variable as boolean or returns a default value
func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// MustLoad loads configuration and panics on error
func MustLoad() *Config {
	config, err := Load()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}
	return config
}

// Print prints the configuration (without sensitive data)
func (c *Config) Print() {
	fmt.Printf("Application Configuration:\n")
	fmt.Printf("  Environment: %s\n", c.App.Environment)
	fmt.Printf("  Name: %s\n", c.App.Name)
	fmt.Printf("  Version: %s\n", c.App.Version)
	fmt.Printf("Server Configuration:\n")
	fmt.Printf("  Address: %s\n", c.GetServerAddress())
	fmt.Printf("  Read Timeout: %s\n", c.Server.ReadTimeout)
	fmt.Printf("  Write Timeout: %s\n", c.Server.WriteTimeout)
	fmt.Printf("Database Configuration:\n")
	fmt.Printf("  Host: %s:%d\n", c.Database.Host, c.Database.Port)
	fmt.Printf("  Database: %s\n", c.Database.Database)
	fmt.Printf("  User: %s\n", c.Database.User)
	fmt.Printf("  SSL Mode: %s\n", c.Database.SSLMode)
	fmt.Printf("Elasticsearch Configuration:\n")
	fmt.Printf("  URL: %s\n", c.Elastic.URL)
	fmt.Printf("  Index: %s\n", c.Elastic.Index)
	fmt.Printf("Logger Configuration:\n")
	fmt.Printf("  Level: %s\n", c.Logger.Level)
	fmt.Printf("  Format: %s\n", c.Logger.Format)
}
