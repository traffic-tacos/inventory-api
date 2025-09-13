package server

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	appconfig "github.com/traffictacos/inventory-api/internal/config"
	"github.com/traffictacos/inventory-api/internal/repo"
	"github.com/traffictacos/inventory-api/internal/service"
	"github.com/traffictacos/inventory-api/proto"
)

// Server represents the gRPC server
type Server struct {
	config   *appconfig.Config
	server   *grpc.Server
	listener net.Listener
	service  *service.InventoryService
}

// NewServer creates a new gRPC server
func NewServer(cfg *appconfig.Config) (*Server, error) {
	// Create repository
	repository, err := repo.NewDynamoDBRepository(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create repository: %w", err)
	}

	// Create service
	svc := service.NewInventoryService(repository, cfg)

	// Create gRPC server with interceptors
	server := grpc.NewServer(
		grpc.UnaryInterceptor(unaryInterceptor),
		grpc.MaxConcurrentStreams(uint32(cfg.Server.MaxConcurrency)),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			Time:    cfg.Server.KeepAlivePeriod,
			Timeout: cfg.Server.Timeout,
		}),
	)

	// Register services
	inventoryServer := &inventoryServer{service: svc}
	proto.RegisterInventoryServer(server, inventoryServer)

	// Enable reflection for debugging
	reflection.Register(server)

	return &Server{
		config:  cfg,
		server:  server,
		service: svc,
	}, nil
}

// Start starts the gRPC server
func (s *Server) Start() error {
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.config.Server.Port))
	if err != nil {
		return fmt.Errorf("failed to listen on port %d: %w", s.config.Server.Port, err)
	}

	s.listener = listener
	return s.server.Serve(listener)
}

// Stop stops the gRPC server gracefully
func (s *Server) Stop(ctx context.Context) error {
	done := make(chan struct{})

	go func() {
		s.server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		s.server.Stop()
		return ctx.Err()
	}
}

// unaryInterceptor provides common unary interceptor functionality
func unaryInterceptor(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
	// Set timeout if not already set
	if deadline, ok := ctx.Deadline(); !ok || time.Until(deadline) > 250*time.Millisecond {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 250*time.Millisecond)
		defer cancel()
	}

	// Add tracing/observability here if needed
	start := time.Now()

	resp, err := handler(ctx, req)

	// Log request duration
	duration := time.Since(start)
	fmt.Printf("Method: %s, Duration: %v, Error: %v\n", info.FullMethod, duration, err)

	return resp, err
}

// inventoryServer implements the Inventory gRPC service
type inventoryServer struct {
	proto.UnimplementedInventoryServer
	service *service.InventoryService
}

// CheckAvailability implements the CheckAvailability gRPC method
func (s *inventoryServer) CheckAvailability(ctx context.Context, req *proto.CheckReq) (*proto.CheckRes, error) {
	resp, err := s.service.CheckAvailability(ctx, req)
	if err != nil {
		return nil, mapErrorToGRPC(err)
	}
	return resp, nil
}

// CommitReservation implements the CommitReservation gRPC method
func (s *inventoryServer) CommitReservation(ctx context.Context, req *proto.CommitReq) (*proto.CommitRes, error) {
	resp, err := s.service.CommitReservation(ctx, req)
	if err != nil {
		return nil, mapErrorToGRPC(err)
	}
	return resp, nil
}

// ReleaseHold implements the ReleaseHold gRPC method
func (s *inventoryServer) ReleaseHold(ctx context.Context, req *proto.ReleaseReq) (*proto.ReleaseRes, error) {
	resp, err := s.service.ReleaseHold(ctx, req)
	if err != nil {
		return nil, mapErrorToGRPC(err)
	}
	return resp, nil
}

// mapErrorToGRPC maps service errors to appropriate gRPC status codes
func mapErrorToGRPC(err error) error {
	if err == nil {
		return nil
	}

	switch err.Error() {
	case "insufficient inventory", "seat not available", "one or more seats are not available":
		return status.Error(codes.Aborted, err.Error())
	case "inventory not found", "seat not found":
		return status.Error(codes.NotFound, err.Error())
	default:
		// Check for specific error patterns
		if strings.Contains(err.Error(), "insufficient") || strings.Contains(err.Error(), "not available") {
			return status.Error(codes.Aborted, err.Error())
		}
		if strings.Contains(err.Error(), "not found") {
			return status.Error(codes.NotFound, err.Error())
		}
		return status.Error(codes.Internal, err.Error())
	}
}
