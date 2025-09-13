package service

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/google/uuid"
	appconfig "github.com/traffictacos/inventory-api/internal/config"
	"github.com/traffictacos/inventory-api/internal/repo"
	"github.com/traffictacos/inventory-api/proto"
)

// InventoryService handles inventory business logic
type InventoryService struct {
	repo   *repo.DynamoDBRepository
	config *appconfig.Config
}

// NewInventoryService creates a new inventory service
func NewInventoryService(repo *repo.DynamoDBRepository, cfg *appconfig.Config) *InventoryService {
	return &InventoryService{
		repo:   repo,
		config: cfg,
	}
}

// CommitReservation commits a reservation by reducing inventory
// This operation guarantees zero oversell through conditional updates/transactions
func (s *InventoryService) CommitReservation(ctx context.Context, req *proto.CommitReq) (*proto.CommitRes, error) {
	// Generate order ID
	orderID := fmt.Sprintf("ord_%s", uuid.New().String()[:12])

	// Check idempotency
	idempotencyKey := fmt.Sprintf("commit:%s", req.ReservationId)
	idempotencyItem, err := s.repo.GetIdempotency(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}

	// If already processed, return the previous result
	if idempotencyItem != nil {
		return &proto.CommitRes{
			OrderId: idempotencyItem.Operation, // Store order_id in operation field
			Status:  "CONFIRMED",
		}, nil
	}

	// Determine inventory type and process accordingly
	if len(req.SeatIds) > 0 {
		// Seat-based inventory
		return s.commitSeatReservation(ctx, req, orderID, idempotencyKey)
	} else {
		// Quantity-based inventory
		return s.commitQuantityReservation(ctx, req, orderID, idempotencyKey)
	}
}

// commitQuantityReservation handles quantity-based inventory reservation
func (s *InventoryService) commitQuantityReservation(ctx context.Context, req *proto.CommitReq, orderID, idempotencyKey string) (*proto.CommitRes, error) {
	// Build update expression for conditional quantity reduction
	updateExpr := "SET remaining = remaining - :qty, version = version + 1, updated_at = :updated_at"
	conditionExpr := "remaining >= :qty AND version = :current_version"

	// Get current inventory to check version
	currentInventory, err := s.repo.GetInventory(ctx, req.EventId)
	if err != nil {
		return nil, fmt.Errorf("failed to get current inventory: %w", err)
	}

	exprValues := map[string]types.AttributeValue{
		":qty": &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", req.Qty),
		},
		":current_version": &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", currentInventory.Version),
		},
		":updated_at": &types.AttributeValueMemberS{
			Value: time.Now().Format(time.RFC3339),
		},
	}

	// Attempt conditional update
	err = s.repo.UpdateInventoryConditionally(ctx, req.EventId, updateExpr, conditionExpr, exprValues, nil)
	if err != nil {
		// Check if it's a conditional check failure (insufficient inventory)
		var conditionalCheckFailed *types.ConditionalCheckFailedException
		if err == conditionalCheckFailed {
			return nil, fmt.Errorf("insufficient inventory for event %s", req.EventId)
		}
		return nil, fmt.Errorf("failed to commit quantity reservation: %w", err)
	}

	// Store idempotency record
	err = s.repo.PutIdempotency(ctx, &repo.IdempotencyItem{
		Key:       idempotencyKey,
		Operation: orderID,
		EventID:   req.EventId,
		CreatedAt: time.Now(),
	})
	if err != nil {
		// Log error but don't fail the operation since the inventory was already committed
		// In production, you might want to implement a retry mechanism or dead letter queue
		fmt.Printf("Warning: failed to store idempotency record: %v\n", err)
	}

	return &proto.CommitRes{
		OrderId: orderID,
		Status:  "CONFIRMED",
	}, nil
}

// commitSeatReservation handles seat-based inventory reservation
func (s *InventoryService) commitSeatReservation(ctx context.Context, req *proto.CommitReq, orderID, idempotencyKey string) (*proto.CommitRes, error) {
	seatIDs := make([]string, len(req.SeatIds))
	for i, seatRef := range req.SeatIds {
		seatIDs[i] = seatRef.SeatId
	}

	// Get current seat statuses
	seats, err := s.repo.GetSeats(ctx, req.EventId, seatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get seats: %w", err)
	}

	// Check if all seats are available or held by this reservation
	for _, seat := range seats {
		if seat.Status != "AVAILABLE" && seat.ReservationID != req.ReservationId {
			return nil, fmt.Errorf("seat %s is not available", seat.SeatID)
		}
	}

	// Prepare seat updates for transaction
	var seatUpdates []*repo.SeatItem
	for _, seatID := range seatIDs {
		seatUpdates = append(seatUpdates, &repo.SeatItem{
			EventID:       req.EventId,
			SeatID:        seatID,
			Status:        "SOLD",
			ReservationID: req.ReservationId,
			UpdatedAt:     time.Now(),
		})
	}

	// Build condition expression for transaction
	conditionExpr := "attribute_not_exists(seat_id) OR (status = :available OR (status = :hold AND reservation_id = :reservation_id))"

	exprValues := map[string]types.AttributeValue{
		":available": &types.AttributeValueMemberS{
			Value: "AVAILABLE",
		},
		":hold": &types.AttributeValueMemberS{
			Value: "HOLD",
		},
		":reservation_id": &types.AttributeValueMemberS{
			Value: req.ReservationId,
		},
	}

	// Execute transaction
	err = s.repo.TransactWriteSeats(ctx, seatUpdates, conditionExpr, exprValues)
	if err != nil {
		var conditionalCheckFailed *types.ConditionalCheckFailedException
		if err == conditionalCheckFailed {
			return nil, fmt.Errorf("one or more seats are not available for event %s", req.EventId)
		}
		return nil, fmt.Errorf("failed to commit seat reservation: %w", err)
	}

	// Store idempotency record
	err = s.repo.PutIdempotency(ctx, &repo.IdempotencyItem{
		Key:       idempotencyKey,
		Operation: orderID,
		EventID:   req.EventId,
		CreatedAt: time.Now(),
	})
	if err != nil {
		fmt.Printf("Warning: failed to store idempotency record: %v\n", err)
	}

	return &proto.CommitRes{
		OrderId: orderID,
		Status:  "CONFIRMED",
	}, nil
}

// ReleaseHold releases a hold on inventory (idempotent operation)
func (s *InventoryService) ReleaseHold(ctx context.Context, req *proto.ReleaseReq) (*proto.ReleaseRes, error) {
	// Check idempotency
	idempotencyKey := fmt.Sprintf("release:%s", req.ReservationId)
	idempotencyItem, err := s.repo.GetIdempotency(ctx, idempotencyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}

	// If already processed, return success (idempotent)
	if idempotencyItem != nil {
		return &proto.ReleaseRes{
			Status: "RELEASED",
		}, nil
	}

	// Determine inventory type and process accordingly
	if len(req.SeatIds) > 0 {
		// Seat-based inventory
		return s.releaseSeatHold(ctx, req, idempotencyKey)
	} else {
		// Quantity-based inventory
		return s.releaseQuantityHold(ctx, req, idempotencyKey)
	}
}

// releaseQuantityHold handles quantity-based inventory hold release
func (s *InventoryService) releaseQuantityHold(ctx context.Context, req *proto.ReleaseReq, idempotencyKey string) (*proto.ReleaseRes, error) {
	// For quantity-based, we simply increment the remaining count
	// This is a simplified implementation - in practice, you might want to track holds separately
	updateExpr := "SET remaining = remaining + :qty, updated_at = :updated_at"

	exprValues := map[string]types.AttributeValue{
		":qty": &types.AttributeValueMemberN{
			Value: fmt.Sprintf("%d", req.Qty),
		},
		":updated_at": &types.AttributeValueMemberS{
			Value: time.Now().Format(time.RFC3339),
		},
	}

	err := s.repo.UpdateInventoryConditionally(ctx, req.EventId, updateExpr, "", exprValues, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to release quantity hold: %w", err)
	}

	// Store idempotency record
	err = s.repo.PutIdempotency(ctx, &repo.IdempotencyItem{
		Key:       idempotencyKey,
		Operation: "RELEASED",
		EventID:   req.EventId,
		CreatedAt: time.Now(),
	})
	if err != nil {
		fmt.Printf("Warning: failed to store idempotency record: %w", err)
	}

	return &proto.ReleaseRes{
		Status: "RELEASED",
	}, nil
}

// releaseSeatHold handles seat-based inventory hold release
func (s *InventoryService) releaseSeatHold(ctx context.Context, req *proto.ReleaseReq, idempotencyKey string) (*proto.ReleaseRes, error) {
	seatIDs := make([]string, len(req.SeatIds))
	for i, seatRef := range req.SeatIds {
		seatIDs[i] = seatRef.SeatId
	}

	// Get current seat statuses
	seats, err := s.repo.GetSeats(ctx, req.EventId, seatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get seats: %w", err)
	}

	// Prepare seat updates for transaction
	var seatUpdates []*repo.SeatItem
	for _, seat := range seats {
		// Only update if the seat is held by this reservation
		if seat.ReservationID == req.ReservationId {
			seatUpdates = append(seatUpdates, &repo.SeatItem{
				EventID:       req.EventId,
				SeatID:        seat.SeatID,
				Status:        "AVAILABLE",
				ReservationID: "", // Clear reservation ID
				UpdatedAt:     time.Now(),
			})
		}
	}

	if len(seatUpdates) == 0 {
		// No seats to release, consider it successful (idempotent)
		return &proto.ReleaseRes{
			Status: "RELEASED",
		}, nil
	}

	// Execute transaction without condition (since we're releasing)
	err = s.repo.TransactWriteSeats(ctx, seatUpdates, "", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to release seat hold: %w", err)
	}

	// Store idempotency record
	err = s.repo.PutIdempotency(ctx, &repo.IdempotencyItem{
		Key:       idempotencyKey,
		Operation: "RELEASED",
		EventID:   req.EventId,
		CreatedAt: time.Now(),
	})
	if err != nil {
		fmt.Printf("Warning: failed to store idempotency record: %w", err)
	}

	return &proto.ReleaseRes{
		Status: "RELEASED",
	}, nil
}

// CheckAvailability checks if inventory is available for the given request
func (s *InventoryService) CheckAvailability(ctx context.Context, req *proto.CheckReq) (*proto.CheckRes, error) {
	if len(req.SeatIds) > 0 {
		// Seat-based availability check
		return s.checkSeatAvailability(ctx, req)
	} else {
		// Quantity-based availability check
		return s.checkQuantityAvailability(ctx, req)
	}
}

// checkQuantityAvailability handles quantity-based availability check
func (s *InventoryService) checkQuantityAvailability(ctx context.Context, req *proto.CheckReq) (*proto.CheckRes, error) {
	inventory, err := s.repo.GetInventory(ctx, req.EventId)
	if err != nil {
		return nil, fmt.Errorf("failed to get inventory: %w", err)
	}

	if inventory.Remaining >= req.Qty {
		return &proto.CheckRes{
			Available: true,
		}, nil
	}

	return &proto.CheckRes{
		Available: false,
	}, nil
}

// checkSeatAvailability handles seat-based availability check
func (s *InventoryService) checkSeatAvailability(ctx context.Context, req *proto.CheckReq) (*proto.CheckRes, error) {
	seatIDs := make([]string, len(req.SeatIds))
	for i, seatRef := range req.SeatIds {
		seatIDs[i] = seatRef.SeatId
	}

	seats, err := s.repo.GetSeats(ctx, req.EventId, seatIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to get seats: %w", err)
	}

	var unavailableSeats []string
	for _, seat := range seats {
		if seat.Status != "AVAILABLE" {
			unavailableSeats = append(unavailableSeats, seat.SeatID)
		}
	}

	return &proto.CheckRes{
		Available:        len(unavailableSeats) == 0,
		UnavailableSeats: unavailableSeats,
	}, nil
}
