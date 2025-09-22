package server

import (
	"context"
	"time"

	"github.com/traffictacos/inventory-api/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Interceptors struct {
	logger  *observability.Logger
	metrics *observability.Metrics
}

func NewInterceptors(logger *observability.Logger, metrics *observability.Metrics) *Interceptors {
	return &Interceptors{
		logger:  logger,
		metrics: metrics,
	}
}

// LoggingUnaryInterceptor logs gRPC requests and responses
func (i *Interceptors) LoggingUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		i.logger.LogGRPCRequest(ctx, info.FullMethod, req)

		resp, err := handler(ctx, req)

		latency := time.Since(start)
		i.logger.LogGRPCResponse(ctx, info.FullMethod, resp, err, latency.Milliseconds())

		return resp, err
	}
}

// MetricsUnaryInterceptor records gRPC metrics
func (i *Interceptors) MetricsUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		statusCode := codes.OK
		if err != nil {
			if s, ok := status.FromError(err); ok {
				statusCode = s.Code()
			} else {
				statusCode = codes.Internal
			}
		}

		i.metrics.RecordGRPCRequest(info.FullMethod, statusCode.String(), duration)

		return resp, err
	}
}

// RecoveryUnaryInterceptor recovers from panics
func (i *Interceptors) RecoveryUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				i.logger.WithContext(ctx).Error("gRPC handler panicked",
					observability.StringField("method", info.FullMethod),
					observability.StringField("panic", r.(string)),
				)
				err = status.Error(codes.Internal, "internal server error")
			}
		}()

		return handler(ctx, req)
	}
}

// TimeoutUnaryInterceptor enforces request timeouts
func (i *Interceptors) TimeoutUnaryInterceptor(timeout time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		return handler(ctx, req)
	}
}

// ValidationUnaryInterceptor validates requests
func (i *Interceptors) ValidationUnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// Basic validation can be added here
		// For now, we'll just pass through to the handler
		return handler(ctx, req)
	}
}