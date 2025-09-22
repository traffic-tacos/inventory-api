package observability

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type Metrics struct {
	// gRPC server metrics
	GRPCRequestDuration *prometheus.HistogramVec
	GRPCRequestTotal    *prometheus.CounterVec

	// DynamoDB metrics
	DynamoDBLatency *prometheus.HistogramVec
	DynamoDBRCU     *prometheus.CounterVec
	DynamoDBWCU     *prometheus.CounterVec

	// Business metrics
	InventoryConflicts *prometheus.CounterVec
	IdempotentHits     *prometheus.CounterVec
}

func NewMetrics() *Metrics {
	return &Metrics{
		// gRPC metrics
		GRPCRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "grpc_server_handling_seconds",
				Help:    "Histogram of response latency (seconds) of gRPC method handlers.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "status"},
		),

		GRPCRequestTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "grpc_server_requests_total",
				Help: "Total number of gRPC requests handled.",
			},
			[]string{"method", "status"},
		),

		// DynamoDB metrics
		DynamoDBLatency: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "dynamodb_latency_seconds",
				Help:    "Histogram of DynamoDB operation latency.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
			[]string{"operation", "table", "status"},
		),

		DynamoDBRCU: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "dynamodb_consumed_rcu_total",
				Help: "Total consumed read capacity units.",
			},
			[]string{"table"},
		),

		DynamoDBWCU: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "dynamodb_consumed_wcu_total",
				Help: "Total consumed write capacity units.",
			},
			[]string{"table"},
		),

		// Business metrics
		InventoryConflicts: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "inventory_conflicts_total",
				Help: "Total number of inventory conflicts (oversell attempts).",
			},
			[]string{"type", "event_id"},
		),

		IdempotentHits: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "idempotent_hit_total",
				Help: "Total number of idempotent request hits.",
			},
			[]string{"method"},
		),
	}
}

func (m *Metrics) RecordGRPCRequest(method string, status string, duration time.Duration) {
	m.GRPCRequestDuration.WithLabelValues(method, status).Observe(duration.Seconds())
	m.GRPCRequestTotal.WithLabelValues(method, status).Inc()
}

func (m *Metrics) RecordDynamoDBOperation(operation, table, status string, duration time.Duration, rcu, wcu float64) {
	m.DynamoDBLatency.WithLabelValues(operation, table, status).Observe(duration.Seconds())
	if rcu > 0 {
		m.DynamoDBRCU.WithLabelValues(table).Add(rcu)
	}
	if wcu > 0 {
		m.DynamoDBWCU.WithLabelValues(table).Add(wcu)
	}
}

func (m *Metrics) RecordInventoryConflict(conflictType, eventID string) {
	m.InventoryConflicts.WithLabelValues(conflictType, eventID).Inc()
}

func (m *Metrics) RecordIdempotentHit(method string) {
	m.IdempotentHits.WithLabelValues(method).Inc()
}