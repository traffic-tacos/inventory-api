package config

import (
	"os"
	"strconv"
	"time"
)

// Config holds all configuration for the application
type Config struct {
	Server        ServerConfig
	AWS           AWSConfig
	DynamoDB      DynamoDBConfig
	Idempotency   IdempotencyConfig
	Observability ObservabilityConfig
}

// ServerConfig holds server-related configuration
type ServerConfig struct {
	Port            int           `json:"port"`
	Timeout         time.Duration `json:"timeout"`
	MaxConcurrency  int           `json:"max_concurrency"`
	KeepAlivePeriod time.Duration `json:"keep_alive_period"`
}

// AWSConfig holds AWS-related configuration
type AWSConfig struct {
	Region  string `json:"region"`
	Profile string `json:"profile,omitempty"`
}

// DynamoDBConfig holds DynamoDB configuration
type DynamoDBConfig struct {
	TableInventory string        `json:"table_inventory"`
	TableSeats     string        `json:"table_seats"`
	MaxRetries     int           `json:"max_retries"`
	Timeout        time.Duration `json:"timeout"`
}

// IdempotencyConfig holds idempotency configuration
type IdempotencyConfig struct {
	TTLDuration time.Duration `json:"ttl_duration"`
	CacheSize   int           `json:"cache_size"`
}

// ObservabilityConfig holds observability configuration
type ObservabilityConfig struct {
	ServiceName    string `json:"service_name"`
	ServiceVersion string `json:"service_version"`
	OTLPEndpoint   string `json:"otlp_endpoint"`
	LogLevel       string `json:"log_level"`
	MetricsPort    int    `json:"metrics_port"`
}

// Load loads configuration from environment variables with defaults
func Load() (*Config, error) {
	return &Config{
		Server: ServerConfig{
			Port:            getEnvAsInt("GRPC_PORT", 8080),
			Timeout:         getEnvAsDuration("GRPC_TIMEOUT", 250*time.Millisecond),
			MaxConcurrency:  getEnvAsInt("GRPC_MAX_CONCURRENCY", 1000),
			KeepAlivePeriod: getEnvAsDuration("GRPC_KEEP_ALIVE_PERIOD", 30*time.Second),
		},
		AWS: AWSConfig{
			Region:  getEnv("AWS_REGION", "ap-northeast-2"),
			Profile: getEnv("AWS_PROFILE", ""),
		},
		DynamoDB: DynamoDBConfig{
			TableInventory: getEnv("DDB_TABLE_INVENTORY", "inventory"),
			TableSeats:     getEnv("DDB_TABLE_SEATS", "inventory_seats"),
			MaxRetries:     getEnvAsInt("DDB_MAX_RETRIES", 3),
			Timeout:        getEnvAsDuration("DDB_TIMEOUT", 200*time.Millisecond),
		},
		Idempotency: IdempotencyConfig{
			TTLDuration: getEnvAsDuration("IDEMPOTENCY_TTL_SECONDS", 300*time.Second),
			CacheSize:   getEnvAsInt("IDEMPOTENCY_CACHE_SIZE", 10000),
		},
		Observability: ObservabilityConfig{
			ServiceName:    getEnv("SERVICE_NAME", "inventory-api"),
			ServiceVersion: getEnv("SERVICE_VERSION", "1.0.0"),
			OTLPEndpoint:   getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317"),
			LogLevel:       getEnv("LOG_LEVEL", "info"),
			MetricsPort:    getEnvAsInt("METRICS_PORT", 9090),
		},
	}, nil
}

// getEnv gets an environment variable or returns a default value
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvAsInt gets an environment variable as int or returns a default value
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
