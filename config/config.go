package config

import (
	"os"
)

type Config struct {
	Port        string
	Environment string
	LogLevel    string
	ExternalAPI string
}

func Load() *Config {
	return &Config{
		Port:        getEnv("PORT", "8080"),
		Environment: getEnv("ENVIRONMENT", "development"),
		LogLevel:    getEnv("LOG_LEVEL", "info"),
		ExternalAPI: getEnv("EXTERNAL_API_URL", "https://api.example.com"),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
