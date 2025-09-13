package observability

import (
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/traffictacos/inventory-api/internal/config"
)

// Metrics holds all Prometheus metrics
type Metrics struct {
	// gRPC metrics
	GRPCRequestsTotal    *prometheus.CounterVec
	GRPCRequestDuration  *prometheus.HistogramVec
	GRPCActiveRequests   prometheus.Gauge

	// Business logic metrics
	CommitReservationsTotal    *prometheus.CounterVec
	ReleaseHoldsTotal         *prometheus.CounterVec
	CheckAvailabilityTotal    *prometheus.CounterVec
	InventoryConflictsTotal   *prometheus.CounterVec

	// DynamoDB metrics
	DynamoDBLatency          *prometheus.HistogramVec
	DynamoDBRequestsTotal    *prometheus.CounterVec

	// Idempotency metrics
	IdempotencyHitsTotal     *prometheus.CounterVec
	IdempotencyMissesTotal   *prometheus.CounterVec
}

// NewMetrics creates a new metrics instance
func NewMetrics() *Metrics {
	return &Metrics{
		GRPCRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_requests_total",
				Help: "Total number of gRPC requests",
			},
			[]string{"method", "status"},
		),

		GRPCRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_request_duration_seconds",
				Help:    "Duration of gRPC requests",
				Buckets: prometheus.DefBuckets,
			},
			[]string{"method"},
		),

		GRPCActiveRequests: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "grpc_active_requests",
				Help: "Number of active gRPC requests",
			},
		),

		CommitReservationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "inventory_commit_reservations_total",
				Help: "Total number of reservation commits",
			},
			[]string{"inventory_type", "status"}, // quantity, seat
		),

		ReleaseHoldsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "inventory_release_holds_total",
				Help: "Total number of hold releases",
			},
			[]string{"inventory_type", "status"},
		),

		CheckAvailabilityTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "inventory_check_availability_total",
				Help: "Total number of availability checks",
			},
			[]string{"inventory_type", "result"}, // available, unavailable
		),

		InventoryConflictsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "inventory_conflicts_total",
				Help: "Total number of inventory conflicts (oversell attempts)",
			},
			[]string{"conflict_type"}, // quantity, seat
		),

		DynamoDBLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "dynamodb_operation_duration_seconds",
				Help:    "Duration of DynamoDB operations",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5},
			},
			[]string{"operation", "table"},
		),

		DynamoDBRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "dynamodb_requests_total",
				Help: "Total number of DynamoDB requests",
			},
			[]string{"operation", "table", "status"},
		),

		IdempotencyHitsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "idempotency_hits_total",
				Help: "Total number of idempotency cache hits",
			},
			[]string{"operation_type"}, // commit, release
		),

		IdempotencyMissesTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "idempotency_misses_total",
				Help: "Total number of idempotency cache misses",
			},
			[]string{"operation_type"},
		),
	}
}

// StartMetricsServer starts the Prometheus metrics HTTP server
func (m *Metrics) StartMetricsServer(cfg *config.Config) error {
	http.Handle("/metrics", promhttp.Handler())
	return http.ListenAndServe(fmt.Sprintf(":%d", cfg.Observability.MetricsPort), nil)
}

// RecordGRPCRequest records a gRPC request
func (m *Metrics) RecordGRPCRequest(method, status string, duration time.Duration) {
	m.GRPCRequestsTotal.WithLabelValues(method, status).Inc()
	m.GRPCRequestDuration.WithLabelValues(method).Observe(duration.Seconds())
}

// IncrementActiveRequests increments the active requests gauge
func (m *Metrics) IncrementActiveRequests() {
	m.GRPCActiveRequests.Inc()
}

// DecrementActiveRequests decrements the active requests gauge
func (m *Metrics) DecrementActiveRequests() {
	m.GRPCActiveRequests.Dec()
}

// RecordCommitReservation records a reservation commit
func (m *Metrics) RecordCommitReservation(inventoryType, status string) {
	m.CommitReservationsTotal.WithLabelValues(inventoryType, status).Inc()
}

// RecordReleaseHold records a hold release
func (m *Metrics) RecordReleaseHold(inventoryType, status string) {
	m.ReleaseHoldsTotal.WithLabelValues(inventoryType, status).Inc()
}

// RecordCheckAvailability records an availability check
func (m *Metrics) RecordCheckAvailability(inventoryType, result string) {
	m.CheckAvailabilityTotal.WithLabelValues(inventoryType, result).Inc()
}

// RecordInventoryConflict records an inventory conflict
func (m *Metrics) RecordInventoryConflict(conflictType string) {
	m.InventoryConflictsTotal.WithLabelValues(conflictType).Inc()
}

// RecordDynamoDBOperation records a DynamoDB operation
func (m *Metrics) RecordDynamoDBOperation(operation, table, status string, duration time.Duration) {
	m.DynamoDBLatency.WithLabelValues(operation, table).Observe(duration.Seconds())
	m.DynamoDBRequestsTotal.WithLabelValues(operation, table, status).Inc()
}

// RecordIdempotencyHit records an idempotency cache hit
func (m *Metrics) RecordIdempotencyHit(operationType string) {
	m.IdempotencyHitsTotal.WithLabelValues(operationType).Inc()
}

// RecordIdempotencyMiss records an idempotency cache miss
func (m *Metrics) RecordIdempotencyMiss(operationType string) {
	m.IdempotencyMissesTotal.WithLabelValues(operationType).Inc()
}
