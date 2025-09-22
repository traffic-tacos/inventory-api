package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Server settings
	GRPCPort string
	LogLevel string

	// AWS settings
	AWSRegion string

	// DynamoDB settings
	DDBTableInventory    string
	DDBTableSeats        string
	DDBTableIdempotency  string

	// Idempotency settings
	IdempotencyTTLSeconds int

	// OpenTelemetry settings
	OTELExporterOTLPEndpoint string
	OTELServiceName          string
	OTELServiceVersion       string

	// Performance settings
	MaxConcurrentStreams     int
	ServerTimeout            time.Duration
	IdempotencyCacheTTL      time.Duration
	InventoryMode            string // "quantity" or "seat"
	UseOptimisticLocking     bool
}

func Load() *Config {
	return &Config{
		// Server
		GRPCPort: getEnv("GRPC_PORT", "8020"),
		LogLevel: getEnv("LOG_LEVEL", "info"),

		// AWS
		AWSRegion: getEnv("AWS_REGION", "ap-northeast-2"),

		// DynamoDB
		DDBTableInventory:   getEnv("DYNAMODB_TABLE_INVENTORY", "inventory"),
		DDBTableSeats:       getEnv("DYNAMODB_TABLE_SEATS", "inventory_seats"),
		DDBTableIdempotency: getEnv("DYNAMODB_TABLE_IDEMPOTENCY", "idempotency"),

		// Idempotency
		IdempotencyTTLSeconds: getEnvAsInt("IDEMPOTENCY_TTL_SECONDS", 300),

		// OpenTelemetry
		OTELExporterOTLPEndpoint: getEnv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://otel-collector:4317"),
		OTELServiceName:          getEnv("OTEL_SERVICE_NAME", "inventory-api"),
		OTELServiceVersion:       getEnv("OTEL_SERVICE_VERSION", "1.0.0"),

		// Performance
		MaxConcurrentStreams: getEnvAsInt("MAX_CONCURRENT_STREAMS", 1000),
		ServerTimeout:        time.Duration(getEnvAsInt("SERVER_TIMEOUT_MS", 250)) * time.Millisecond,
		IdempotencyCacheTTL:  time.Duration(getEnvAsInt("IDEMPOTENCY_CACHE_TTL_SECONDS", 300)) * time.Second,
		InventoryMode:        getEnv("INVENTORY_MODE", "quantity"), // quantity|seat
		UseOptimisticLocking: getEnvAsBool("USE_OPTIMISTIC_LOCKING", true),
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvAsInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intValue, err := strconv.Atoi(value); err == nil {
			return intValue
		}
	}
	return defaultValue
}

func getEnvAsBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		if boolValue, err := strconv.ParseBool(value); err == nil {
			return boolValue
		}
	}
	return defaultValue
}