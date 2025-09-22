package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	reservationv1 "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"
	"github.com/traffictacos/inventory-api/internal/observability"
	"github.com/traffictacos/inventory-api/internal/repo"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type InventoryService struct {
	repo                  repo.RepositoryInterface
	logger                *observability.Logger
	metrics               *observability.Metrics
	inventoryMode         string // "quantity" or "seat"
	idempotencyTTLSeconds int
}

func NewInventoryService(repo repo.RepositoryInterface, logger *observability.Logger, metrics *observability.Metrics, inventoryMode string, idempotencyTTLSeconds int) *InventoryService {
	return &InventoryService{
		repo:                  repo,
		logger:                logger,
		metrics:               metrics,
		inventoryMode:         inventoryMode,
		idempotencyTTLSeconds: idempotencyTTLSeconds,
	}
}

func (s *InventoryService) CheckAvailability(ctx context.Context, req *reservationv1.CheckAvailabilityRequest) (*reservationv1.CheckAvailabilityResponse, error) {
	ctx, span := observability.StartSpan(ctx, "service", "CheckAvailability")
	defer span.End()

	observability.AddSpanAttributes(span, map[string]interface{}{
		"event_id": req.EventId,
		"quantity": req.Quantity,
		"seats":    len(req.SeatIds),
	})

	s.logger.WithContext(ctx).Info("CheckAvailability request",
		observability.StringField("event_id", req.EventId),
		observability.Int32Field("quantity", req.Quantity),
		observability.IntField("seat_count", len(req.SeatIds)),
	)

	// Validate request
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}

	// Determine if this is seat-based or quantity-based
	isSeatBased := len(req.SeatIds) > 0

	if isSeatBased {
		return s.checkSeatAvailability(ctx, req.EventId, req.SeatIds)
	}

	if req.Quantity <= 0 {
		return nil, status.Error(codes.InvalidArgument, "quantity must be greater than 0 for quantity-based check")
	}

	return s.checkQuantityAvailability(ctx, req.EventId, req.Quantity)
}

func (s *InventoryService) CommitReservation(ctx context.Context, req *reservationv1.CommitReservationRequest) (*reservationv1.CommitReservationResponse, error) {
	ctx, span := observability.StartSpan(ctx, "service", "CommitReservation")
	defer span.End()

	observability.AddSpanAttributes(span, map[string]interface{}{
		"reservation_id":    req.ReservationId,
		"event_id":          req.EventId,
		"quantity":          req.Quantity,
		"seats":             len(req.SeatIds),
		"payment_intent_id": req.PaymentIntentId,
	})

	s.logger.WithContext(ctx).Info("CommitReservation request",
		observability.StringField("reservation_id", req.ReservationId),
		observability.StringField("event_id", req.EventId),
		observability.Int32Field("quantity", req.Quantity),
		observability.IntField("seat_count", len(req.SeatIds)),
		observability.StringField("payment_intent_id", req.PaymentIntentId),
	)

	// Validate request
	if req.ReservationId == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation_id is required")
	}
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}

	// Check idempotency
	if idempotentResp, err := s.checkIdempotency(ctx, req.ReservationId, "CommitReservation"); err != nil {
		return nil, status.Error(codes.Internal, "failed to check idempotency")
	} else if idempotentResp != nil {
		var resp reservationv1.CommitReservationResponse
		if err := json.Unmarshal([]byte(idempotentResp.Response), &resp); err == nil {
			s.logger.WithContext(ctx).Info("Returning idempotent response",
				observability.StringField("reservation_id", req.ReservationId),
			)
			return &resp, nil
		}
	}

	// Determine if this is seat-based or quantity-based
	isSeatBased := len(req.SeatIds) > 0

	var err error
	var orderID string

	if isSeatBased {
		orderID, err = s.commitSeatReservation(ctx, req.EventId, req.SeatIds, req.ReservationId)
	} else {
		if req.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be greater than 0 for quantity-based commit")
		}
		orderID, err = s.commitQuantityReservation(ctx, req.EventId, req.Quantity, req.ReservationId)
	}

	if err != nil {
		s.logger.WithContext(ctx).Error("Failed to commit reservation",
			observability.StringField("reservation_id", req.ReservationId),
			observability.ErrorField(err),
		)

		// Check if it's a conflict error
		if strings.Contains(err.Error(), "conditional check failed") || strings.Contains(err.Error(), "ConditionalCheckFailedException") {
			return nil, status.Error(codes.Aborted, "inventory conflict: seats unavailable or insufficient quantity")
		}

		return nil, status.Error(codes.Internal, "failed to commit reservation")
	}

	response := &reservationv1.CommitReservationResponse{
		OrderId: orderID,
		Status:  reservationv1.CommitStatus_COMMIT_STATUS_SUCCESS,
	}

	// Save idempotency record
	if respBytes, err := json.Marshal(response); err == nil {
		if err := s.repo.SaveIdempotency(ctx, req.ReservationId, "CommitReservation", string(respBytes), s.idempotencyTTLSeconds); err != nil {
			s.logger.WithContext(ctx).Warn("Failed to save idempotency record",
				observability.StringField("reservation_id", req.ReservationId),
				observability.ErrorField(err),
			)
		}
	}

	s.logger.WithContext(ctx).Info("Reservation committed successfully",
		observability.StringField("reservation_id", req.ReservationId),
		observability.StringField("order_id", orderID),
	)

	return response, nil
}

func (s *InventoryService) ReleaseHold(ctx context.Context, req *reservationv1.ReleaseHoldRequest) (*reservationv1.ReleaseHoldResponse, error) {
	ctx, span := observability.StartSpan(ctx, "service", "ReleaseHold")
	defer span.End()

	observability.AddSpanAttributes(span, map[string]interface{}{
		"reservation_id": req.ReservationId,
		"event_id":       req.EventId,
		"quantity":       req.Quantity,
		"seats":          len(req.SeatIds),
	})

	s.logger.WithContext(ctx).Info("ReleaseHold request",
		observability.StringField("reservation_id", req.ReservationId),
		observability.StringField("event_id", req.EventId),
		observability.Int32Field("quantity", req.Quantity),
		observability.IntField("seat_count", len(req.SeatIds)),
	)

	// Validate request
	if req.ReservationId == "" {
		return nil, status.Error(codes.InvalidArgument, "reservation_id is required")
	}
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}

	// Check idempotency
	if idempotentResp, err := s.checkIdempotency(ctx, req.ReservationId, "ReleaseHold"); err != nil {
		return nil, status.Error(codes.Internal, "failed to check idempotency")
	} else if idempotentResp != nil {
		var resp reservationv1.ReleaseHoldResponse
		if err := json.Unmarshal([]byte(idempotentResp.Response), &resp); err == nil {
			s.logger.WithContext(ctx).Info("Returning idempotent response",
				observability.StringField("reservation_id", req.ReservationId),
			)
			return &resp, nil
		}
	}

	// Determine if this is seat-based or quantity-based
	isSeatBased := len(req.SeatIds) > 0

	var err error

	if isSeatBased {
		err = s.releaseSeatHold(ctx, req.EventId, req.SeatIds, req.ReservationId)
	} else {
		if req.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be greater than 0 for quantity-based release")
		}
		err = s.releaseQuantityHold(ctx, req.EventId, req.Quantity, req.ReservationId)
	}

	if err != nil {
		// For release operations, we treat missing/already released as success (idempotent)
		if strings.Contains(err.Error(), "conditional check failed") {
			s.logger.WithContext(ctx).Info("Hold already released or not found, treating as success",
				observability.StringField("reservation_id", req.ReservationId),
			)
		} else {
			s.logger.WithContext(ctx).Error("Failed to release hold",
				observability.StringField("reservation_id", req.ReservationId),
				observability.ErrorField(err),
			)
			return nil, status.Error(codes.Internal, "failed to release hold")
		}
	}

	response := &reservationv1.ReleaseHoldResponse{
		Status: reservationv1.ReleaseStatus_RELEASE_STATUS_SUCCESS,
	}

	// Save idempotency record
	if respBytes, err := json.Marshal(response); err == nil {
		if err := s.repo.SaveIdempotency(ctx, req.ReservationId, "ReleaseHold", string(respBytes), s.idempotencyTTLSeconds); err != nil {
			s.logger.WithContext(ctx).Warn("Failed to save idempotency record",
				observability.StringField("reservation_id", req.ReservationId),
				observability.ErrorField(err),
			)
		}
	}

	s.logger.WithContext(ctx).Info("Hold released successfully",
		observability.StringField("reservation_id", req.ReservationId),
	)

	return response, nil
}

// Helper methods

func (s *InventoryService) checkQuantityAvailability(ctx context.Context, eventID string, qty int32) (*reservationv1.CheckAvailabilityResponse, error) {
	inventory, err := s.repo.GetInventory(ctx, eventID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get inventory")
	}

	if inventory == nil {
		return &reservationv1.CheckAvailabilityResponse{
			Available: false,
		}, nil
	}

	available := inventory.Remaining >= qty

	return &reservationv1.CheckAvailabilityResponse{
		Available: available,
	}, nil
}

func (s *InventoryService) checkSeatAvailability(ctx context.Context, eventID string, seatIDs []string) (*reservationv1.CheckAvailabilityResponse, error) {
	seats, err := s.repo.GetSeats(ctx, eventID, seatIDs)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to get seats")
	}

	// Create a map of existing seats
	existingSeats := make(map[string]*repo.SeatRecord)
	for i := range seats {
		seat := &seats[i]
		parts := strings.Split(seat.EventSeatID, "#")
		if len(parts) == 2 {
			existingSeats[parts[1]] = seat
		}
	}

	var unavailableSeats []string
	allAvailable := true

	for _, seatID := range seatIDs {
		seat, exists := existingSeats[seatID]
		if !exists || seat.Status != "AVAILABLE" {
			unavailableSeats = append(unavailableSeats, seatID)
			allAvailable = false
		}
	}

	return &reservationv1.CheckAvailabilityResponse{
		Available:           allAvailable,
		UnavailableSeatIds: unavailableSeats,
	}, nil
}

func (s *InventoryService) commitQuantityReservation(ctx context.Context, eventID string, qty int32, reservationID string) (string, error) {
	err := s.repo.UpdateQuantityInventory(ctx, eventID, qty, reservationID)
	if err != nil {
		return "", err
	}

	// Generate order ID
	orderID := fmt.Sprintf("ord_%s_%s", eventID, reservationID)
	return orderID, nil
}

func (s *InventoryService) commitSeatReservation(ctx context.Context, eventID string, seatIDs []string, reservationID string) (string, error) {
	err := s.repo.CommitSeatReservation(ctx, eventID, seatIDs, reservationID)
	if err != nil {
		return "", err
	}

	// Generate order ID
	orderID := fmt.Sprintf("ord_%s_%s", eventID, reservationID)
	return orderID, nil
}

func (s *InventoryService) releaseQuantityHold(ctx context.Context, eventID string, qty int32, reservationID string) error {
	return s.repo.ReleaseQuantityInventory(ctx, eventID, qty, reservationID)
}

func (s *InventoryService) releaseSeatHold(ctx context.Context, eventID string, seatIDs []string, reservationID string) error {
	return s.repo.ReleaseSeats(ctx, eventID, seatIDs, reservationID)
}

func (s *InventoryService) checkIdempotency(ctx context.Context, reservationID, method string) (*repo.IdempotencyRecord, error) {
	return s.repo.CheckIdempotency(ctx, reservationID, method)
}

// Helper function for logging fields (assuming these exist in observability package)
func StringField(key, value string) observability.Field {
	return observability.Field{Key: key, Value: value}
}

func Int32Field(key string, value int32) observability.Field {
	return observability.Field{Key: key, Value: value}
}

func IntField(key string, value int) observability.Field {
	return observability.Field{Key: key, Value: value}
}

func ErrorField(err error) observability.Field {
	return observability.Field{Key: "error", Value: err.Error()}
}