package server

import (
	"context"
	"fmt"
	"net"
	"time"

	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	reservationv1 "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"
	"github.com/traffictacos/inventory-api/internal/config"
	"github.com/traffictacos/inventory-api/internal/observability"
	"github.com/traffictacos/inventory-api/internal/service"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

type GRPCServer struct {
	reservationv1.UnimplementedInventoryServiceServer
	inventoryService *service.InventoryService
	logger           *observability.Logger
	metrics          *observability.Metrics
}

func NewGRPCServer(inventoryService *service.InventoryService, logger *observability.Logger, metrics *observability.Metrics) *GRPCServer {
	return &GRPCServer{
		inventoryService: inventoryService,
		logger:           logger,
		metrics:          metrics,
	}
}

func (s *GRPCServer) CheckAvailability(ctx context.Context, req *reservationv1.CheckAvailabilityRequest) (*reservationv1.CheckAvailabilityResponse, error) {
	return s.inventoryService.CheckAvailability(ctx, req)
}

func (s *GRPCServer) CommitReservation(ctx context.Context, req *reservationv1.CommitReservationRequest) (*reservationv1.CommitReservationResponse, error) {
	return s.inventoryService.CommitReservation(ctx, req)
}

func (s *GRPCServer) ReleaseHold(ctx context.Context, req *reservationv1.ReleaseHoldRequest) (*reservationv1.ReleaseHoldResponse, error) {
	return s.inventoryService.ReleaseHold(ctx, req)
}

func StartGRPCServer(ctx context.Context, cfg *config.Config, grpcServer *GRPCServer, logger *observability.Logger, metrics *observability.Metrics) error {
	// Create interceptors
	interceptors := NewInterceptors(logger, metrics)

	// Configure server options
	opts := []grpc.ServerOption{
		grpc.ChainUnaryInterceptor(
			interceptors.RecoveryUnaryInterceptor(),
			interceptors.TimeoutUnaryInterceptor(cfg.ServerTimeout),
			interceptors.ValidationUnaryInterceptor(),
			interceptors.LoggingUnaryInterceptor(),
			interceptors.MetricsUnaryInterceptor(),
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    60 * time.Second,
			Timeout: 5 * time.Second,
		}),
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
		grpc.MaxConcurrentStreams(uint32(cfg.MaxConcurrentStreams)),
	}

	// Create gRPC server
	server := grpc.NewServer(opts...)

	// Register services
	reservationv1.RegisterInventoryServiceServer(server, grpcServer)

	// Enable reflection for development
	reflection.Register(server)

	// Create listener
	lis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return fmt.Errorf("failed to listen on port %s: %w", cfg.GRPCPort, err)
	}

	logger.Info("Starting gRPC server",
		observability.StringField("port", cfg.GRPCPort),
		observability.IntField("max_concurrent_streams", cfg.MaxConcurrentStreams),
	)

	// Start metrics server
	go startMetricsServer(logger)

	// Start gRPC server
	if err := server.Serve(lis); err != nil {
		return fmt.Errorf("failed to serve gRPC server: %w", err)
	}

	return nil
}

func startMetricsServer(logger *observability.Logger) {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", healthCheck)

	server := &http.Server{
		Addr:    ":8021",
		Handler: mux,
	}

	logger.Info("Starting metrics server",
		observability.StringField("port", "8021"),
		observability.StringField("endpoints", "/metrics, /health"),
	)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("Metrics server failed",
			observability.ErrorField(err),
		)
	}
}

func healthCheck(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
