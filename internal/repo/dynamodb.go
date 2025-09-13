package repo

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	appconfig "github.com/traffictacos/inventory-api/internal/config"
)

// DynamoDBRepository handles DynamoDB operations
type DynamoDBRepository struct {
	client         *dynamodb.Client
	tableInventory string
	tableSeats     string
}

// NewDynamoDBRepository creates a new DynamoDB repository
func NewDynamoDBRepository(cfg *appconfig.Config) (*DynamoDBRepository, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	client := dynamodb.NewFromConfig(awsCfg)

	return &DynamoDBRepository{
		client:         client,
		tableInventory: cfg.DynamoDB.TableInventory,
		tableSeats:     cfg.DynamoDB.TableSeats,
	}, nil
}

// InventoryItem represents an inventory item in DynamoDB
type InventoryItem struct {
	EventID    string                 `dynamodbav:"event_id"`
	Remaining  int32                  `dynamodbav:"remaining"`
	Version    int32                  `dynamodbav:"version"`
	UpdatedAt  time.Time              `dynamodbav:"updated_at"`
	TotalSeats int32                  `dynamodbav:"total_seats,omitempty"`
	Sections   map[string]interface{} `dynamodbav:"sections,omitempty"`
}

// SeatItem represents a seat item in DynamoDB
type SeatItem struct {
	EventID       string    `dynamodbav:"event_id"`
	SeatID        string    `dynamodbav:"seat_id"`
	Status        string    `dynamodbav:"status"` // AVAILABLE, HOLD, SOLD
	ReservationID string    `dynamodbav:"reservation_id,omitempty"`
	UpdatedAt     time.Time `dynamodbav:"updated_at"`
}

// IdempotencyItem represents an idempotency item in DynamoDB
type IdempotencyItem struct {
	Key       string    `dynamodbav:"key"`
	Operation string    `dynamodbav:"operation"`
	EventID   string    `dynamodbav:"event_id"`
	CreatedAt time.Time `dynamodbav:"created_at"`
}

// GetInventory retrieves inventory information for an event
func (r *DynamoDBRepository) GetInventory(ctx context.Context, eventID string) (*InventoryItem, error) {
	result, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableInventory),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get inventory: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("inventory not found for event: %s", eventID)
	}

	item := &InventoryItem{}
	err = unmarshalDynamoItem(result.Item, item)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal inventory item: %w", err)
	}

	return item, nil
}

// PutInventory stores or updates inventory information
func (r *DynamoDBRepository) PutInventory(ctx context.Context, item *InventoryItem) error {
	dynamoItem, err := marshalDynamoItem(item)
	if err != nil {
		return fmt.Errorf("failed to marshal inventory item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(r.tableInventory),
		Item:      dynamoItem,
	})

	if err != nil {
		return fmt.Errorf("failed to put inventory: %w", err)
	}

	return nil
}

// UpdateInventoryConditionally updates inventory with conditional expression
func (r *DynamoDBRepository) UpdateInventoryConditionally(ctx context.Context, eventID string, updateExpr string, conditionExpr string, exprValues map[string]types.AttributeValue, exprNames map[string]string) error {
	_, err := r.client.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(r.tableInventory),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
		},
		UpdateExpression:          aws.String(updateExpr),
		ConditionExpression:       aws.String(conditionExpr),
		ExpressionAttributeValues: exprValues,
		ExpressionAttributeNames:  exprNames,
	})

	if err != nil {
		return fmt.Errorf("failed to update inventory conditionally: %w", err)
	}

	return nil
}

// GetSeat retrieves seat information
func (r *DynamoDBRepository) GetSeat(ctx context.Context, eventID, seatID string) (*SeatItem, error) {
	result, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(r.tableSeats),
		Key: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
			"seat_id":  &types.AttributeValueMemberS{Value: seatID},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get seat: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("seat not found: %s", seatID)
	}

	item := &SeatItem{}
	err = unmarshalDynamoItem(result.Item, item)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal seat item: %w", err)
	}

	return item, nil
}

// GetSeats retrieves multiple seats information
func (r *DynamoDBRepository) GetSeats(ctx context.Context, eventID string, seatIDs []string) ([]*SeatItem, error) {
	if len(seatIDs) == 0 {
		return nil, nil
	}

	keys := make([]map[string]types.AttributeValue, len(seatIDs))
	for i, seatID := range seatIDs {
		keys[i] = map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
			"seat_id":  &types.AttributeValueMemberS{Value: seatID},
		}
	}

	result, err := r.client.BatchGetItem(ctx, &dynamodb.BatchGetItemInput{
		RequestItems: map[string]types.KeysAndAttributes{
			r.tableSeats: {
				Keys: keys,
			},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to batch get seats: %w", err)
	}

	seats := make([]*SeatItem, 0, len(result.Responses[r.tableSeats]))
	for _, item := range result.Responses[r.tableSeats] {
		seat := &SeatItem{}
		err = unmarshalDynamoItem(item, seat)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal seat item: %w", err)
		}
		seats = append(seats, seat)
	}

	return seats, nil
}

// TransactWriteSeats performs transactional write on multiple seats
func (r *DynamoDBRepository) TransactWriteSeats(ctx context.Context, items []*SeatItem, conditionExpr string, exprValues map[string]types.AttributeValue) error {
	if len(items) == 0 {
		return nil
	}

	var transactItems []types.TransactWriteItem

	for _, item := range items {
		dynamoItem, err := marshalDynamoItem(item)
		if err != nil {
			return fmt.Errorf("failed to marshal seat item: %w", err)
		}

		transactItems = append(transactItems, types.TransactWriteItem{
			Put: &types.Put{
				TableName:           aws.String(r.tableSeats),
				Item:                dynamoItem,
				ConditionExpression: aws.String(conditionExpr),
			},
		})
	}

	// Set expression attribute values if provided
	if len(exprValues) > 0 {
		for i := range transactItems {
			if transactItems[i].Put != nil {
				transactItems[i].Put.ExpressionAttributeValues = exprValues
			}
		}
	}

	_, err := r.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: transactItems,
	})

	if err != nil {
		return fmt.Errorf("failed to transact write seats: %w", err)
	}

	return nil
}

// PutIdempotency stores idempotency information
func (r *DynamoDBRepository) PutIdempotency(ctx context.Context, item *IdempotencyItem) error {
	dynamoItem, err := marshalDynamoItem(item)
	if err != nil {
		return fmt.Errorf("failed to marshal idempotency item: %w", err)
	}

	_, err = r.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String("idempotency"), // You might want to make this configurable
		Item:      dynamoItem,
	})

	if err != nil {
		return fmt.Errorf("failed to put idempotency: %w", err)
	}

	return nil
}

// GetIdempotency retrieves idempotency information
func (r *DynamoDBRepository) GetIdempotency(ctx context.Context, key string) (*IdempotencyItem, error) {
	result, err := r.client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String("idempotency"),
		Key: map[string]types.AttributeValue{
			"key": &types.AttributeValueMemberS{Value: key},
		},
	})

	if err != nil {
		return nil, fmt.Errorf("failed to get idempotency: %w", err)
	}

	if result.Item == nil {
		return nil, nil // Not found
	}

	item := &IdempotencyItem{}
	err = unmarshalDynamoItem(result.Item, item)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal idempotency item: %w", err)
	}

	return item, nil
}

// marshalDynamoItem marshals a Go struct to DynamoDB attribute values
func marshalDynamoItem(item interface{}) (map[string]types.AttributeValue, error) {
	return attributevalue.MarshalMap(item)
}

// unmarshalDynamoItem unmarshals DynamoDB attribute values to a Go struct
func unmarshalDynamoItem(item map[string]types.AttributeValue, out interface{}) error {
	return attributevalue.UnmarshalMap(item, out)
}
