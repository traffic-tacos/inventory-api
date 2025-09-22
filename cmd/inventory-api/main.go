package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	appconfig "github.com/traffictacos/inventory-api/internal/config"
	"github.com/traffictacos/inventory-api/internal/observability"
	"github.com/traffictacos/inventory-api/internal/repo"
	"github.com/traffictacos/inventory-api/internal/server"
	"github.com/traffictacos/inventory-api/internal/service"
)

func main() {
	ctx := context.Background()

	// Load configuration
	cfg := appconfig.Load()

	// Initialize logger
	logger, err := observability.NewLogger(cfg.LogLevel)
	if err != nil {
		log.Fatalf("Failed to initialize logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("Starting inventory-api",
		observability.StringField("version", cfg.OTELServiceVersion),
		observability.StringField("log_level", cfg.LogLevel),
		observability.StringField("grpc_port", cfg.GRPCPort),
		observability.StringField("aws_region", cfg.AWSRegion),
		observability.StringField("inventory_mode", cfg.InventoryMode),
	)

	// Initialize metrics
	metrics := observability.NewMetrics()

	// Initialize OpenTelemetry tracing
	tracerProvider, err := observability.InitTracing(ctx, cfg.OTELServiceName, cfg.OTELServiceVersion, cfg.OTELExporterOTLPEndpoint)
	if err != nil {
		logger.Error("Failed to initialize tracing",
			observability.ErrorField(err),
		)
	} else {
		defer func() {
			if err := tracerProvider.Shutdown(ctx); err != nil {
				logger.Error("Failed to shutdown tracer provider",
					observability.ErrorField(err),
				)
			}
		}()
		logger.Info("OpenTelemetry tracing initialized",
			observability.StringField("endpoint", cfg.OTELExporterOTLPEndpoint),
		)
	}

	// Initialize AWS configuration
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.AWSRegion),
	)
	if err != nil {
		logger.Error("Failed to load AWS configuration",
			observability.ErrorField(err),
		)
		os.Exit(1)
	}

	// Initialize DynamoDB client
	dynamoClient := dynamodb.NewFromConfig(awsCfg)

	// Initialize repository
	repository := repo.NewRepository(
		dynamoClient,
		cfg.DDBTableInventory,
		cfg.DDBTableSeats,
		cfg.DDBTableIdempotency,
		logger,
		metrics,
		cfg.UseOptimisticLocking,
	)

	// Initialize service
	inventoryService := service.NewInventoryService(
		repository,
		logger,
		metrics,
		cfg.InventoryMode,
		cfg.IdempotencyTTLSeconds,
	)

	// Initialize gRPC server
	grpcServer := server.NewGRPCServer(inventoryService, logger, metrics)

	// Start server in a goroutine
	serverCtx, serverCancel := context.WithCancel(ctx)
	defer serverCancel()

	go func() {
		if err := server.StartGRPCServer(serverCtx, cfg, grpcServer, logger, metrics); err != nil {
			logger.Error("gRPC server failed",
				observability.ErrorField(err),
			)
			os.Exit(1)
		}
	}()

	logger.Info("inventory-api started successfully",
		observability.StringField("grpc_port", cfg.GRPCPort),
		observability.StringField("metrics_port", "8081"),
		observability.StringField("grpcui_command", "grpcui -plaintext localhost:"+cfg.GRPCPort),
	)

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down inventory-api...")

	// Cancel server context to initiate shutdown
	serverCancel()

	logger.Info("inventory-api stopped")
}