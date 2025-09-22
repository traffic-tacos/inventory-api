#!/bin/bash

# Load testing script for inventory-api
set -e

SERVER_HOST=${1:-localhost}
SERVER_PORT=${2:-8080}
RPS=${3:-800}
DURATION=${4:-30s}

echo "Starting load test for inventory-api"
echo "Server: $SERVER_HOST:$SERVER_PORT"
echo "RPS: $RPS"
echo "Duration: $DURATION"

# Check if ghz is installed
if ! command -v ghz &> /dev/null; then
    echo "Error: ghz is not installed. Please install it:"
    echo "go install github.com/bojand/ghz/cmd/ghz@latest"
    exit 1
fi

# Test CheckAvailability endpoint
echo "Testing CheckAvailability..."
ghz --insecure \
    --proto proto/inventory.proto \
    --call inventory.v1.Inventory/CheckAvailability \
    --data '{"event_id":"load-test-event","qty":1}' \
    --rps $RPS \
    --duration $DURATION \
    --connections 10 \
    --concurrency 50 \
    --timeout 5s \
    $SERVER_HOST:$SERVER_PORT

echo ""
echo "Testing CommitReservation..."
ghz --insecure \
    --proto proto/inventory.proto \
    --call inventory.v1.Inventory/CommitReservation \
    --data '{"reservation_id":"load-test-{{.RequestNumber}}","event_id":"load-test-event","qty":1,"payment_intent_id":"pay-{{.RequestNumber}}"}' \
    --rps 100 \
    --duration 10s \
    --connections 5 \
    --concurrency 20 \
    --timeout 5s \
    $SERVER_HOST:$SERVER_PORT

echo ""
echo "Load test completed!"