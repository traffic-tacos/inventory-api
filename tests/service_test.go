package tests

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	reservationv1 "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"
	"github.com/traffictacos/inventory-api/internal/observability"
	"github.com/traffictacos/inventory-api/internal/repo"
	"github.com/traffictacos/inventory-api/internal/service"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockRepository implements repo.RepositoryInterface for testing
type MockRepository struct {
	inventory map[string]*repo.InventoryRecord
	seats     map[string]*repo.SeatRecord
	idempotency map[string]*repo.IdempotencyRecord
}

func NewMockRepository() *MockRepository {
	return &MockRepository{
		inventory:   make(map[string]*repo.InventoryRecord),
		seats:       make(map[string]*repo.SeatRecord),
		idempotency: make(map[string]*repo.IdempotencyRecord),
	}
}

func (m *MockRepository) GetInventory(ctx context.Context, eventID string) (*repo.InventoryRecord, error) {
	if record, exists := m.inventory[eventID]; exists {
		return record, nil
	}
	return nil, nil
}

func (m *MockRepository) UpdateQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error {
	record, exists := m.inventory[eventID]
	if !exists {
		return status.Error(codes.NotFound, "event not found")
	}

	if record.Remaining < qty {
		return status.Error(codes.Aborted, "conditional check failed")
	}

	record.Remaining -= qty
	record.Version++
	return nil
}

func (m *MockRepository) GetSeats(ctx context.Context, eventID string, seatIDs []string) ([]repo.SeatRecord, error) {
	var seats []repo.SeatRecord
	for _, seatID := range seatIDs {
		key := eventID + "#" + seatID
		if seat, exists := m.seats[key]; exists {
			seats = append(seats, *seat)
		}
	}
	return seats, nil
}

func (m *MockRepository) CommitSeatReservation(ctx context.Context, eventID string, seatIDs []string, reservationID string) error {
	for _, seatID := range seatIDs {
		key := eventID + "#" + seatID
		seat, exists := m.seats[key]
		if !exists || seat.Status != "AVAILABLE" {
			return status.Error(codes.Aborted, "seat not available")
		}
		seat.Status = "SOLD"
		seat.ReservationID = reservationID
	}
	return nil
}

func (m *MockRepository) ReleaseSeats(ctx context.Context, eventID string, seatIDs []string, reservationID string) error {
	for _, seatID := range seatIDs {
		key := eventID + "#" + seatID
		if seat, exists := m.seats[key]; exists && seat.ReservationID == reservationID {
			seat.Status = "AVAILABLE"
			seat.ReservationID = ""
		}
	}
	return nil
}

func (m *MockRepository) ReleaseQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error {
	if record, exists := m.inventory[eventID]; exists {
		record.Remaining += qty
		record.Version++
	}
	return nil
}

func (m *MockRepository) CheckIdempotency(ctx context.Context, reservationID, method string) (*repo.IdempotencyRecord, error) {
	key := reservationID + "#" + method
	if record, exists := m.idempotency[key]; exists {
		return record, nil
	}
	return nil, nil
}

func (m *MockRepository) SaveIdempotency(ctx context.Context, reservationID, method, response string, ttlSeconds int) error {
	key := reservationID + "#" + method
	m.idempotency[key] = &repo.IdempotencyRecord{
		ReservationID: reservationID,
		Method:        method,
		Response:      response,
	}
	return nil
}

func setupTestService() (*service.InventoryService, *MockRepository) {
	mockRepo := NewMockRepository()
	logger, _ := observability.NewLogger("debug")
	metrics := NewTestMetrics()

	inventoryService := service.NewInventoryService(
		mockRepo,
		logger,
		metrics,
		"quantity",
		300,
	)

	return inventoryService, mockRepo
}

func NewTestMetrics() *observability.Metrics {
	// Create a separate registry for tests to avoid duplicate registration
	reg := prometheus.NewRegistry()
	factory := promauto.With(reg)

	return &observability.Metrics{
		GRPCRequestDuration: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_grpc_server_handling_seconds",
				Help:    "Test histogram of response latency (seconds) of gRPC method handlers.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			},
			[]string{"method", "status"},
		),
		GRPCRequestTotal: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_grpc_server_requests_total",
				Help: "Test total number of gRPC requests handled.",
			},
			[]string{"method", "status"},
		),
		DynamoDBLatency: factory.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "test_dynamodb_latency_seconds",
				Help:    "Test histogram of DynamoDB operation latency.",
				Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
			},
			[]string{"operation", "table", "status"},
		),
		DynamoDBRCU: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_dynamodb_consumed_rcu_total",
				Help: "Test total consumed read capacity units.",
			},
			[]string{"table"},
		),
		DynamoDBWCU: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_dynamodb_consumed_wcu_total",
				Help: "Test total consumed write capacity units.",
			},
			[]string{"table"},
		),
		InventoryConflicts: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_inventory_conflicts_total",
				Help: "Test total number of inventory conflicts (oversell attempts).",
			},
			[]string{"type", "event_id"},
		),
		IdempotentHits: factory.NewCounterVec(
			prometheus.CounterOpts{
				Name: "test_idempotent_hit_total",
				Help: "Test total number of idempotent request hits.",
			},
			[]string{"method"},
		),
	}
}

func TestCheckAvailability_QuantityMode(t *testing.T) {
	inventoryService, mockRepo := setupTestService()
	ctx := context.Background()

	// Setup test data
	mockRepo.inventory["event1"] = &repo.InventoryRecord{
		EventID:   "event1",
		Remaining: 100,
		Version:   1,
	}

	tests := []struct {
		name      string
		eventID   string
		qty       int32
		expected  bool
	}{
		{
			name:     "sufficient inventory",
			eventID:  "event1",
			qty:      50,
			expected: true,
		},
		{
			name:     "insufficient inventory",
			eventID:  "event1",
			qty:      150,
			expected: false,
		},
		{
			name:     "exact inventory",
			eventID:  "event1",
			qty:      100,
			expected: true,
		},
		{
			name:     "event not found",
			eventID:  "event2",
			qty:      10,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &reservationv1.CheckAvailabilityRequest{
				EventId:  tt.eventID,
				Quantity: tt.qty,
			}

			resp, err := inventoryService.CheckAvailability(ctx, req)
			if err != nil {
				t.Fatalf("CheckAvailability failed: %v", err)
			}

			if resp.Available != tt.expected {
				t.Errorf("Expected available=%v, got %v", tt.expected, resp.Available)
			}
		})
	}
}

func TestCheckAvailability_SeatMode(t *testing.T) {
	inventoryService, mockRepo := setupTestService()
	ctx := context.Background()

	// Setup test data
	mockRepo.seats["event1#A1"] = &repo.SeatRecord{
		EventSeatID:   "event1#A1",
		Status:        "AVAILABLE",
		ReservationID: "",
	}
	mockRepo.seats["event1#A2"] = &repo.SeatRecord{
		EventSeatID:   "event1#A2",
		Status:        "SOLD",
		ReservationID: "rsv123",
	}

	tests := []struct {
		name             string
		eventID          string
		seatIDs          []string
		expectedAvail    bool
		expectedUnavail  []string
	}{
		{
			name:            "all seats available",
			eventID:         "event1",
			seatIDs:         []string{"A1"},
			expectedAvail:   true,
			expectedUnavail: nil,
		},
		{
			name:            "some seats unavailable",
			eventID:         "event1",
			seatIDs:         []string{"A1", "A2"},
			expectedAvail:   false,
			expectedUnavail: []string{"A2"},
		},
		{
			name:            "non-existent seats",
			eventID:         "event1",
			seatIDs:         []string{"A3"},
			expectedAvail:   false,
			expectedUnavail: []string{"A3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &reservationv1.CheckAvailabilityRequest{
				EventId: tt.eventID,
				SeatIds: tt.seatIDs,
			}

			resp, err := inventoryService.CheckAvailability(ctx, req)
			if err != nil {
				t.Fatalf("CheckAvailability failed: %v", err)
			}

			if resp.Available != tt.expectedAvail {
				t.Errorf("Expected available=%v, got %v", tt.expectedAvail, resp.Available)
			}

			if len(resp.UnavailableSeatIds) != len(tt.expectedUnavail) {
				t.Errorf("Expected %d unavailable seats, got %d", len(tt.expectedUnavail), len(resp.UnavailableSeatIds))
			}
		})
	}
}

func TestCommitReservation_QuantityMode(t *testing.T) {
	inventoryService, mockRepo := setupTestService()
	ctx := context.Background()

	// Setup test data
	mockRepo.inventory["event1"] = &repo.InventoryRecord{
		EventID:   "event1",
		Remaining: 100,
		Version:   1,
	}

	tests := []struct {
		name          string
		eventID       string
		qty           int32
		reservationID string
		expectError   bool
		errorCode     codes.Code
	}{
		{
			name:          "successful commit",
			eventID:       "event1",
			qty:           50,
			reservationID: "rsv1",
			expectError:   false,
		},
		{
			name:          "insufficient inventory",
			eventID:       "event1",
			qty:           200,
			reservationID: "rsv2",
			expectError:   true,
			errorCode:     codes.Aborted,
		},
		{
			name:          "missing reservation_id",
			eventID:       "event1",
			qty:           10,
			reservationID: "",
			expectError:   true,
			errorCode:     codes.InvalidArgument,
		},
		{
			name:          "missing event_id",
			eventID:       "",
			qty:           10,
			reservationID: "rsv3",
			expectError:   true,
			errorCode:     codes.InvalidArgument,
		},
		{
			name:          "zero quantity",
			eventID:       "event1",
			qty:           0,
			reservationID: "rsv4",
			expectError:   true,
			errorCode:     codes.InvalidArgument,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &reservationv1.CommitReservationRequest{
				ReservationId:   tt.reservationID,
				EventId:         tt.eventID,
				Quantity:        tt.qty,
				PaymentIntentId: "pay123",
			}

			resp, err := inventoryService.CommitReservation(ctx, req)

			if tt.expectError {
				if err == nil {
					t.Fatal("Expected error but got none")
				}

				if s, ok := status.FromError(err); ok {
					if s.Code() != tt.errorCode {
						t.Errorf("Expected error code %v, got %v", tt.errorCode, s.Code())
					}
				}
			} else {
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}

				if resp.Status != reservationv1.CommitStatus_COMMIT_STATUS_SUCCESS {
					t.Errorf("Expected status COMMIT_STATUS_SUCCESS, got %v", resp.Status)
				}

				if resp.OrderId == "" {
					t.Error("Expected non-empty order ID")
				}
			}
		})
	}
}

func TestReleaseHold_QuantityMode(t *testing.T) {
	inventoryService, mockRepo := setupTestService()
	ctx := context.Background()

	// Setup test data
	mockRepo.inventory["event1"] = &repo.InventoryRecord{
		EventID:   "event1",
		Remaining: 50,
		Version:   1,
	}

	req := &reservationv1.ReleaseHoldRequest{
		ReservationId: "rsv1",
		EventId:       "event1",
		Quantity:      25,
	}

	resp, err := inventoryService.ReleaseHold(ctx, req)
	if err != nil {
		t.Fatalf("ReleaseHold failed: %v", err)
	}

	if resp.Status != reservationv1.ReleaseStatus_RELEASE_STATUS_SUCCESS {
		t.Errorf("Expected status RELEASE_STATUS_SUCCESS, got %v", resp.Status)
	}

	// Verify inventory was updated
	if mockRepo.inventory["event1"].Remaining != 75 {
		t.Errorf("Expected remaining inventory 75, got %d", mockRepo.inventory["event1"].Remaining)
	}
}

func TestIdempotency(t *testing.T) {
	inventoryService, mockRepo := setupTestService()
	ctx := context.Background()

	// Setup test data
	mockRepo.inventory["event1"] = &repo.InventoryRecord{
		EventID:   "event1",
		Remaining: 100,
		Version:   1,
	}

	req := &reservationv1.CommitReservationRequest{
		ReservationId:   "rsv1",
		EventId:         "event1",
		Quantity:        10,
		PaymentIntentId: "pay123",
	}

	// First call
	resp1, err := inventoryService.CommitReservation(ctx, req)
	if err != nil {
		t.Fatalf("First CommitReservation failed: %v", err)
	}

	// Second call with same reservation_id should return same result
	resp2, err := inventoryService.CommitReservation(ctx, req)
	if err != nil {
		t.Fatalf("Second CommitReservation failed: %v", err)
	}

	if resp1.OrderId != resp2.OrderId {
		t.Errorf("Expected same order ID, got %s and %s", resp1.OrderId, resp2.OrderId)
	}

	// Verify inventory was only decremented once
	if mockRepo.inventory["event1"].Remaining != 90 {
		t.Errorf("Expected remaining inventory 90, got %d", mockRepo.inventory["event1"].Remaining)
	}
}