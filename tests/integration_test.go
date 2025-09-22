//go:build integration
// +build integration

package tests

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	reservationv1 "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"
	"github.com/traffictacos/inventory-api/internal/observability"
	"github.com/traffictacos/inventory-api/internal/repo"
	"github.com/traffictacos/inventory-api/internal/service"
)

// Integration tests require LocalStack DynamoDB
const (
	localStackEndpoint = "http://localhost:4566"
	testRegion         = "ap-northeast-2"
	testTableInventory = "test-inventory"
	testTableSeats     = "test-inventory-seats"
	testTableIdemp     = "test-idempotency"
)

func setupIntegrationTest(t *testing.T) (*service.InventoryService, *dynamodb.Client) {
	ctx := context.Background()

	// Configure AWS client for LocalStack
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(testRegion),
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL: localStackEndpoint,
				}, nil
			})),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	client := dynamodb.NewFromConfig(cfg)

	// Create test tables
	createTestTables(ctx, t, client)

	// Initialize components
	logger, _ := observability.NewLogger("debug")
	metrics := observability.NewMetrics()

	repository := repo.NewRepository(
		client,
		testTableInventory,
		testTableSeats,
		testTableIdemp,
		logger,
		metrics,
		true, // use optimistic locking
	)

	inventoryService := service.NewInventoryService(
		repository,
		logger,
		metrics,
		"quantity",
		300,
	)

	return inventoryService, client
}

func createTestTables(ctx context.Context, t *testing.T, client *dynamodb.Client) {
	// Create inventory table
	_, err := client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testTableInventory),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("event_id"),
				KeyType:       types.KeyTypeHash,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("event_id"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Logf("Failed to create inventory table (may already exist): %v", err)
	}

	// Create seats table
	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testTableSeats),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("event_seat_id"),
				KeyType:       types.KeyTypeHash,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("event_seat_id"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Logf("Failed to create seats table (may already exist): %v", err)
	}

	// Create idempotency table
	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(testTableIdemp),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("reservation_id"),
				KeyType:       types.KeyTypeHash,
			},
			{
				AttributeName: aws.String("method"),
				KeyType:       types.KeyTypeRange,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("reservation_id"),
				AttributeType: types.ScalarAttributeTypeS,
			},
			{
				AttributeName: aws.String("method"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		t.Logf("Failed to create idempotency table (may already exist): %v", err)
	}

	// Wait for tables to be ready
	time.Sleep(2 * time.Second)
}

func setupTestInventory(ctx context.Context, t *testing.T, client *dynamodb.Client, eventID string, remaining int32) {
	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(testTableInventory),
		Item: map[string]types.AttributeValue{
			"event_id": &types.AttributeValueMemberS{Value: eventID},
			"remaining": &types.AttributeValueMemberN{Value: aws.String(string(rune(remaining + '0')))},
			"version": &types.AttributeValueMemberN{Value: "1"},
			"updated_at": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
		},
	})
	if err != nil {
		t.Fatalf("Failed to setup test inventory: %v", err)
	}
}

func setupTestSeats(ctx context.Context, t *testing.T, client *dynamodb.Client, eventID string, seatIDs []string, status string) {
	for _, seatID := range seatIDs {
		eventSeatID := eventID + "#" + seatID
		_, err := client.PutItem(ctx, &dynamodb.PutItemInput{
			TableName: aws.String(testTableSeats),
			Item: map[string]types.AttributeValue{
				"event_seat_id": &types.AttributeValueMemberS{Value: eventSeatID},
				"status": &types.AttributeValueMemberS{Value: status},
				"updated_at": &types.AttributeValueMemberS{Value: time.Now().Format(time.RFC3339)},
			},
		})
		if err != nil {
			t.Fatalf("Failed to setup test seat %s: %v", seatID, err)
		}
	}
}

func TestIntegration_QuantityCommitAndRelease(t *testing.T) {
	inventoryService, client := setupIntegrationTest(t)
	ctx := context.Background()

	eventID := "integration-test-event-1"
	reservationID := "rsv-integration-1"

	// Setup test data
	setupTestInventory(ctx, t, client, eventID, 100)

	// Test CommitReservation
	commitReq := &reservationv1.CommitReservationRequest{
		ReservationId:   reservationID,
		EventId:         eventID,
		Quantity:             25,
		PaymentIntentId: "pay-123",
	}

	commitResp, err := inventoryService.CommitReservation(ctx, commitReq)
	if err != nil {
		t.Fatalf("CommitReservation failed: %v", err)
	}

	if commitResp.Status != reservationv1.CommitStatus_COMMIT_STATUS_SUCCESS {
		t.Errorf("Expected status CONFIRMED, got %s", commitResp.Status)
	}

	// Verify inventory was decremented
	checkReq := &reservationv1.CheckAvailabilityRequest{
		EventId: eventID,
		Quantity:     76, // Should fail since we committed 25 out of 100
	}

	checkResp, err := inventoryService.CheckAvailability(ctx, checkReq)
	if err != nil {
		t.Fatalf("CheckAvailability failed: %v", err)
	}

	if checkResp.Available {
		t.Error("Expected availability check to fail after commit")
	}

	// Test ReleaseHold
	releaseReq := &reservationv1.ReleaseHoldRequest{
		ReservationId: reservationID,
		EventId:       eventID,
		Quantity:           25,
	}

	releaseResp, err := inventoryService.ReleaseHold(ctx, releaseReq)
	if err != nil {
		t.Fatalf("ReleaseHold failed: %v", err)
	}

	if releaseResp.Status != reservationv1.ReleaseStatus_RELEASE_STATUS_SUCCESS {
		t.Errorf("Expected status RELEASED, got %s", releaseResp.Status)
	}

	// Verify inventory was restored
	checkResp2, err := inventoryService.CheckAvailability(ctx, checkReq)
	if err != nil {
		t.Fatalf("CheckAvailability after release failed: %v", err)
	}

	if !checkResp2.Available {
		t.Error("Expected availability check to succeed after release")
	}
}

func TestIntegration_ConcurrentCommits(t *testing.T) {
	inventoryService, client := setupIntegrationTest(t)
	ctx := context.Background()

	eventID := "integration-test-event-concurrent"

	// Setup test data with limited inventory
	setupTestInventory(ctx, t, client, eventID, 10)

	// Simulate concurrent commit attempts
	results := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func(index int) {
			req := &reservationv1.CommitReservationRequest{
				ReservationId:   fmt.Sprintf("rsv-concurrent-%d", index),
				EventId:         eventID,
				Quantity:             3,
				PaymentIntentId: fmt.Sprintf("pay-%d", index),
			}

			_, err := inventoryService.CommitReservation(ctx, req)
			results <- err
		}(i)
	}

	// Collect results
	successCount := 0
	conflictCount := 0

	for i := 0; i < 5; i++ {
		err := <-results
		if err == nil {
			successCount++
		} else {
			// Check if it's an expected conflict
			if strings.Contains(err.Error(), "inventory conflict") {
				conflictCount++
			} else {
				t.Errorf("Unexpected error: %v", err)
			}
		}
	}

	// We should have some successes and some conflicts
	// With 10 inventory and 5 concurrent requests of 3 each, we should have 3 successes max
	if successCount == 0 {
		t.Error("Expected at least one successful commit")
	}

	if successCount > 3 {
		t.Errorf("Expected at most 3 successful commits, got %d", successCount)
	}

	t.Logf("Concurrent test results: %d successes, %d conflicts", successCount, conflictCount)
}

func TestIntegration_SeatCommitAndRelease(t *testing.T) {
	inventoryService, client := setupIntegrationTest(t)
	ctx := context.Background()

	eventID := "integration-test-event-seats"
	reservationID := "rsv-seats-1"
	seatIDs := []string{"A1", "A2", "A3"}

	// Setup test seats
	setupTestSeats(ctx, t, client, eventID, seatIDs, "AVAILABLE")

	// Test seat-based commit
	commitReq := &reservationv1.CommitReservationRequest{
		ReservationId:   reservationID,
		EventId:         eventID,
		SeatIds:         seatIDs,
		PaymentIntentId: "pay-seats-123",
	}

	commitResp, err := inventoryService.CommitReservation(ctx, commitReq)
	if err != nil {
		t.Fatalf("Seat CommitReservation failed: %v", err)
	}

	if commitResp.Status != reservationv1.CommitStatus_COMMIT_STATUS_SUCCESS {
		t.Errorf("Expected status CONFIRMED, got %s", commitResp.Status)
	}

	// Verify seats are no longer available
	checkReq := &reservationv1.CheckAvailabilityRequest{
		EventId: eventID,
		SeatIds: seatIDs,
	}

	checkResp, err := inventoryService.CheckAvailability(ctx, checkReq)
	if err != nil {
		t.Fatalf("CheckAvailability failed: %v", err)
	}

	if checkResp.Available {
		t.Error("Expected seats to be unavailable after commit")
	}

	// Test seat release
	releaseReq := &reservationv1.ReleaseHoldRequest{
		ReservationId: reservationID,
		EventId:       eventID,
		SeatIds:       seatIDs,
	}

	releaseResp, err := inventoryService.ReleaseHold(ctx, releaseReq)
	if err != nil {
		t.Fatalf("Seat ReleaseHold failed: %v", err)
	}

	if releaseResp.Status != reservationv1.ReleaseStatus_RELEASE_STATUS_SUCCESS {
		t.Errorf("Expected status RELEASED, got %s", releaseResp.Status)
	}

	// Verify seats are available again
	checkResp2, err := inventoryService.CheckAvailability(ctx, checkReq)
	if err != nil {
		t.Fatalf("CheckAvailability after release failed: %v", err)
	}

	if !checkResp2.Available {
		t.Error("Expected seats to be available after release")
	}
}