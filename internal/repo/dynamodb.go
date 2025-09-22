package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/traffictacos/inventory-api/internal/observability"
)

type DynamoDBClient interface {
	GetItem(ctx context.Context, params *dynamodb.GetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.GetItemOutput, error)
	UpdateItem(ctx context.Context, params *dynamodb.UpdateItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateItemOutput, error)
	TransactWriteItems(ctx context.Context, params *dynamodb.TransactWriteItemsInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TransactWriteItemsOutput, error)
	Query(ctx context.Context, params *dynamodb.QueryInput, optFns ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error)
	PutItem(ctx context.Context, params *dynamodb.PutItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.PutItemOutput, error)
}

type RepositoryInterface interface {
	GetInventory(ctx context.Context, eventID string) (*InventoryRecord, error)
	UpdateQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error
	GetSeats(ctx context.Context, eventID string, seatIDs []string) ([]SeatRecord, error)
	CommitSeatReservation(ctx context.Context, eventID string, seatIDs []string, reservationID string) error
	ReleaseSeats(ctx context.Context, eventID string, seatIDs []string, reservationID string) error
	ReleaseQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error
	CheckIdempotency(ctx context.Context, reservationID, method string) (*IdempotencyRecord, error)
	SaveIdempotency(ctx context.Context, reservationID, method, response string, ttlSeconds int) error
}

type Repository struct {
	client                DynamoDBClient
	inventoryTable        string
	seatsTable            string
	idempotencyTable      string
	logger                *observability.Logger
	metrics               *observability.Metrics
	useOptimisticLocking  bool
}

type InventoryRecord struct {
	EventID     string            `dynamodbav:"event_id"`
	Remaining   int32             `dynamodbav:"remaining"`
	Version     int64             `dynamodbav:"version"`
	UpdatedAt   string            `dynamodbav:"updated_at"`
	SectionData map[string]int32  `dynamodbav:"section_remaining,omitempty"`
}

type SeatRecord struct {
	EventSeatID   string `dynamodbav:"event_seat_id"` // event_id#seat_id
	Status        string `dynamodbav:"status"`        // AVAILABLE|HOLD|SOLD
	ReservationID string `dynamodbav:"reservation_id,omitempty"`
	UpdatedAt     string `dynamodbav:"updated_at"`
}

type IdempotencyRecord struct {
	ReservationID string `dynamodbav:"reservation_id"`
	Method        string `dynamodbav:"method"`
	Response      string `dynamodbav:"response"`
	TTL           int64  `dynamodbav:"ttl"`
	CreatedAt     string `dynamodbav:"created_at"`
}

func NewRepository(client DynamoDBClient, inventoryTable, seatsTable, idempotencyTable string, logger *observability.Logger, metrics *observability.Metrics, useOptimisticLocking bool) *Repository {
	return &Repository{
		client:               client,
		inventoryTable:       inventoryTable,
		seatsTable:           seatsTable,
		idempotencyTable:     idempotencyTable,
		logger:               logger,
		metrics:              metrics,
		useOptimisticLocking: useOptimisticLocking,
	}
}

func (r *Repository) GetInventory(ctx context.Context, eventID string) (*InventoryRecord, error) {
	ctx, span := observability.StartSpan(ctx, "repo", "GetInventory")
	defer span.End()

	start := time.Now()

	input := &dynamodb.GetItemInput{
		TableName: aws.String(r.inventoryTable),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
		ConsistentRead: aws.Bool(true),
	}

	result, err := r.client.GetItem(ctx, input)
	latency := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
		observability.RecordError(span, err)
	}

	r.metrics.RecordDynamoDBOperation("GetItem", r.inventoryTable, status, latency, 1, 0)
	r.logger.LogDynamoDBOperation(ctx, "GetItem", r.inventoryTable, latency.Milliseconds(), 1, 0, err)

	if err != nil {
		return nil, fmt.Errorf("failed to get inventory for event %s: %w", eventID, err)
	}

	if result.Item == nil {
		return nil, nil // Not found
	}

	var record InventoryRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal inventory record: %w", err)
	}

	return &record, nil
}

func (r *Repository) UpdateQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error {
	ctx, span := observability.StartSpan(ctx, "repo", "UpdateQuantityInventory")
	defer span.End()

	start := time.Now()

	updateExpr := "SET remaining = remaining - :qty, version = version + :inc, updated_at = :now"
	conditionExpr := "remaining >= :qty"

	expressionAttributeValues := map[string]types.AttributeValue{
		":qty": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", qty)},
		":inc": &types.AttributeValueMemberN{Value: "1"},
		":now": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
	}

	if r.useOptimisticLocking {
		// Add version check for optimistic locking
		conditionExpr += " AND attribute_exists(version)"
	}

	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(r.inventoryTable),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
		UpdateExpression:          aws.String(updateExpr),
		ConditionExpression:       aws.String(conditionExpr),
		ExpressionAttributeValues: expressionAttributeValues,
	}

	_, err := r.client.UpdateItem(ctx, input)
	latency := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
		observability.RecordError(span, err)

		// Check if it's a conditional check failure (inventory conflict)
		if err != nil && fmt.Sprintf("%T", err) == "*types.ConditionalCheckFailedException" {
			r.metrics.RecordInventoryConflict("quantity", eventID)
		}
	}

	r.metrics.RecordDynamoDBOperation("UpdateItem", r.inventoryTable, status, latency, 0, 1)
	r.logger.LogDynamoDBOperation(ctx, "UpdateItem", r.inventoryTable, latency.Milliseconds(), 0, 1, err)

	if err != nil {
		return fmt.Errorf("failed to update quantity inventory for event %s: %w", eventID, err)
	}

	return nil
}

func (r *Repository) GetSeats(ctx context.Context, eventID string, seatIDs []string) ([]SeatRecord, error) {
	ctx, span := observability.StartSpan(ctx, "repo", "GetSeats")
	defer span.End()

	var seats []SeatRecord
	start := time.Now()

	// Query each seat individually for simplicity
	// In production, you might want to use BatchGetItem for efficiency
	for _, seatID := range seatIDs {
		eventSeatID := fmt.Sprintf("%s#%s", eventID, seatID)

		input := &dynamodb.GetItemInput{
			TableName: aws.String(r.seatsTable),
			Key: map[string]types.AttributeValue{
				"event_seat_id": &types.AttributeValueMemberS{Value: eventSeatID},
			},
			ConsistentRead: aws.Bool(true),
		}

		result, err := r.client.GetItem(ctx, input)
		if err != nil {
			latency := time.Since(start)
			r.metrics.RecordDynamoDBOperation("GetItem", r.seatsTable, "error", latency, 1, 0)
			observability.RecordError(span, err)
			return nil, fmt.Errorf("failed to get seat %s: %w", seatID, err)
		}

		if result.Item != nil {
			var seat SeatRecord
			if err := attributevalue.UnmarshalMap(result.Item, &seat); err != nil {
				return nil, fmt.Errorf("failed to unmarshal seat record: %w", err)
			}
			seats = append(seats, seat)
		}
	}

	latency := time.Since(start)
	r.metrics.RecordDynamoDBOperation("GetItem", r.seatsTable, "success", latency, float64(len(seatIDs)), 0)
	r.logger.LogDynamoDBOperation(ctx, "GetItem", r.seatsTable, latency.Milliseconds(), int64(len(seatIDs)), 0, nil)

	return seats, nil
}

func (r *Repository) CommitSeatReservation(ctx context.Context, eventID string, seatIDs []string, reservationID string) error {
	ctx, span := observability.StartSpan(ctx, "repo", "CommitSeatReservation")
	defer span.End()

	start := time.Now()

	var transactItems []types.TransactWriteItem

	// Create transaction items for each seat
	for _, seatID := range seatIDs {
		eventSeatID := fmt.Sprintf("%s#%s", eventID, seatID)

		// Update seat status to SOLD
		transactItems = append(transactItems, types.TransactWriteItem{
			Update: &types.Update{
				TableName: aws.String(r.seatsTable),
				Key: map[string]types.AttributeValue{
					"event_seat_id": &types.AttributeValueMemberS{Value: eventSeatID},
				},
				UpdateExpression: aws.String("SET #status = :sold, reservation_id = :rid, updated_at = :now"),
				ConditionExpression: aws.String("(attribute_not_exists(reservation_id) AND #status = :available) OR (reservation_id = :rid AND #status = :hold)"),
				ExpressionAttributeNames: map[string]string{
					"#status": "status",
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":sold":      &types.AttributeValueMemberS{Value: "SOLD"},
					":available": &types.AttributeValueMemberS{Value: "AVAILABLE"},
					":hold":      &types.AttributeValueMemberS{Value: "HOLD"},
					":rid":       &types.AttributeValueMemberS{Value: reservationID},
					":now":       &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
				},
			},
		})
	}

	input := &dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	}

	_, err := r.client.TransactWriteItems(ctx, input)
	latency := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
		observability.RecordError(span, err)
		r.metrics.RecordInventoryConflict("seat", eventID)
	}

	r.metrics.RecordDynamoDBOperation("TransactWrite", r.seatsTable, status, latency, 0, float64(len(seatIDs)))
	r.logger.LogDynamoDBOperation(ctx, "TransactWrite", r.seatsTable, latency.Milliseconds(), 0, int64(len(seatIDs)), err)

	if err != nil {
		return fmt.Errorf("failed to commit seat reservation: %w", err)
	}

	return nil
}

func (r *Repository) ReleaseSeats(ctx context.Context, eventID string, seatIDs []string, reservationID string) error {
	ctx, span := observability.StartSpan(ctx, "repo", "ReleaseSeats")
	defer span.End()

	start := time.Now()

	var transactItems []types.TransactWriteItem

	// Create transaction items for each seat
	for _, seatID := range seatIDs {
		eventSeatID := fmt.Sprintf("%s#%s", eventID, seatID)

		// Reset seat to AVAILABLE and remove reservation_id
		transactItems = append(transactItems, types.TransactWriteItem{
			Update: &types.Update{
				TableName: aws.String(r.seatsTable),
				Key: map[string]types.AttributeValue{
					"event_seat_id": &types.AttributeValueMemberS{Value: eventSeatID},
				},
				UpdateExpression: aws.String("SET #status = :available, updated_at = :now REMOVE reservation_id"),
				ConditionExpression: aws.String("reservation_id = :rid"),
				ExpressionAttributeNames: map[string]string{
					"#status": "status",
				},
				ExpressionAttributeValues: map[string]types.AttributeValue{
					":available": &types.AttributeValueMemberS{Value: "AVAILABLE"},
					":rid":       &types.AttributeValueMemberS{Value: reservationID},
					":now":       &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
				},
			},
		})
	}

	input := &dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	}

	_, err := r.client.TransactWriteItems(ctx, input)
	latency := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
		observability.RecordError(span, err)
	}

	r.metrics.RecordDynamoDBOperation("TransactWrite", r.seatsTable, status, latency, 0, float64(len(seatIDs)))
	r.logger.LogDynamoDBOperation(ctx, "TransactWrite", r.seatsTable, latency.Milliseconds(), 0, int64(len(seatIDs)), err)

	if err != nil {
		return fmt.Errorf("failed to release seats: %w", err)
	}

	return nil
}

func (r *Repository) ReleaseQuantityInventory(ctx context.Context, eventID string, qty int32, reservationID string) error {
	ctx, span := observability.StartSpan(ctx, "repo", "ReleaseQuantityInventory")
	defer span.End()

	start := time.Now()

	input := &dynamodb.UpdateItemInput{
		TableName: aws.String(r.inventoryTable),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
		UpdateExpression: aws.String("SET remaining = remaining + :qty, version = version + :inc, updated_at = :now"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":qty": &types.AttributeValueMemberN{Value: fmt.Sprintf("%d", qty)},
			":inc": &types.AttributeValueMemberN{Value: "1"},
			":now": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
		},
	}

	_, err := r.client.UpdateItem(ctx, input)
	latency := time.Since(start)

	status := "success"
	if err != nil {
		status = "error"
		observability.RecordError(span, err)
	}

	r.metrics.RecordDynamoDBOperation("UpdateItem", r.inventoryTable, status, latency, 0, 1)
	r.logger.LogDynamoDBOperation(ctx, "UpdateItem", r.inventoryTable, latency.Milliseconds(), 0, 1, err)

	if err != nil {
		return fmt.Errorf("failed to release quantity inventory: %w", err)
	}

	return nil
}

func (r *Repository) CheckIdempotency(ctx context.Context, reservationID, method string) (*IdempotencyRecord, error) {
	ctx, span := observability.StartSpan(ctx, "repo", "CheckIdempotency")
	defer span.End()

	input := &dynamodb.GetItemInput{
		TableName: aws.String(r.idempotencyTable),
		Key: map[string]types.AttributeValue{
			"reservation_id": &types.AttributeValueMemberS{Value: reservationID},
			"method":         &types.AttributeValueMemberS{Value: method},
		},
		ConsistentRead: aws.Bool(true),
	}

	result, err := r.client.GetItem(ctx, input)
	if err != nil {
		observability.RecordError(span, err)
		return nil, fmt.Errorf("failed to check idempotency: %w", err)
	}

	if result.Item == nil {
		return nil, nil // Not found
	}

	var record IdempotencyRecord
	if err := attributevalue.UnmarshalMap(result.Item, &record); err != nil {
		return nil, fmt.Errorf("failed to unmarshal idempotency record: %w", err)
	}

	r.metrics.RecordIdempotentHit(method)
	return &record, nil
}

func (r *Repository) SaveIdempotency(ctx context.Context, reservationID, method, response string, ttlSeconds int) error {
	ctx, span := observability.StartSpan(ctx, "repo", "SaveIdempotency")
	defer span.End()

	record := IdempotencyRecord{
		ReservationID: reservationID,
		Method:        method,
		Response:      response,
		TTL:           time.Now().Add(time.Duration(ttlSeconds) * time.Second).Unix(),
		CreatedAt:     time.Now().Format(time.RFC3339),
	}

	item, err := attributevalue.MarshalMap(record)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency record: %w", err)
	}

	input := &dynamodb.PutItemInput{
		TableName: aws.String(r.idempotencyTable),
		Item:      item,
	}

	_, err = r.client.PutItem(ctx, input)
	if err != nil {
		observability.RecordError(span, err)
		return fmt.Errorf("failed to save idempotency record: %w", err)
	}

	return nil
}