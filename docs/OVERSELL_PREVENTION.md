# 오버셀(Overselling) 방지 메커니즘

## 🎯 목표: 0% 오버셀 보장

Inventory API는 **DynamoDB의 조건부 업데이트(Conditional Updates)와 트랜잭션(TransactWrite)**을 활용하여 **절대 오버셀이 발생하지 않도록** 설계되었습니다.

---

## 📊 두 가지 재고 관리 방식

### 1️⃣ 수량형 재고 (Quantity-based Inventory)

좌석이 지정되지 않은 일반 티켓 (예: 스탠딩존, 자유석)

#### DynamoDB 테이블 구조
```json
{
  "event_id": "evt_2025_1001",
  "remaining": 1000,
  "version": 42,
  "updated_at": "2025-01-15T10:30:00Z"
}
```

#### 오버셀 방지 메커니즘

**핵심: DynamoDB Conditional Update Expression**

```go
// UpdateQuantityInventory 코드 (internal/repo/dynamodb.go:121-173)

updateExpr := "SET remaining = remaining - :qty, version = version + 1, updated_at = :now"
conditionExpr := "remaining >= :qty"  // 🔒 핵심: 재고가 충분할 때만 업데이트

input := &dynamodb.UpdateItemInput{
    TableName: "inventory",
    Key: {"event_id": eventID},
    UpdateExpression: updateExpr,
    ConditionExpression: conditionExpr,  // ⚠️ 조건 실패 시 전체 요청 실패
    ExpressionAttributeValues: {
        ":qty": qty,
        ":inc": 1,
        ":now": time.Now(),
    },
}
```

**동작 원리:**
1. **원자적 비교 및 갱신 (Atomic Compare-And-Set)**
   - DynamoDB가 단일 트랜잭션으로 `remaining >= qty` 조건을 검사하고 업데이트
   - 조건 실패 시 `ConditionalCheckFailedException` 발생 → 예약 실패

2. **낙관적 잠금 (Optimistic Locking) 추가 옵션**
   ```go
   if r.useOptimisticLocking {
       conditionExpr += " AND attribute_exists(version)"
   }
   ```
   - `version` 필드를 매번 증가시켜 동시 업데이트 충돌 감지
   - 두 요청이 동일한 `version`을 읽었다면, 하나만 성공

3. **동시 요청 시나리오 (Race Condition)**
   ```
   Initial State: remaining = 1
   
   ┌─────────────────────────────────────────┐
   │  Request A (qty=1)   Request B (qty=1)  │
   │         │                   │           │
   │         ▼                   ▼           │
   │    Read: remaining=1   Read: remaining=1│
   │         │                   │           │
   │         ▼                   ▼           │
   │    Check: 1 >= 1 ✅    Check: 1 >= 1 ⏳ │
   │    Update: remaining=0   (대기 중)      │
   │         │                   │           │
   │         ✅ 성공              ▼           │
   │                         Check: 0 >= 1 ❌│
   │                         ConditionalCheckFailedException
   └─────────────────────────────────────────┘
   ```
   - **DynamoDB는 직렬화(Serialization)를 보장**
   - 먼저 도착한 요청이 재고를 차감하면, 다음 요청은 조건 실패

---

### 2️⃣ 좌석형 재고 (Seat-based Inventory)

좌석이 지정된 티켓 (예: 콘서트 A-12, B-05 좌석)

#### DynamoDB 테이블 구조
```json
// inventory_seats 테이블
{
  "event_seat_id": "evt_2025_1001#A-12",
  "status": "AVAILABLE",  // AVAILABLE | HOLD | SOLD
  "reservation_id": null,
  "updated_at": "2025-01-15T10:30:00Z"
}
```

#### 오버셀 방지 메커니즘

**핵심: DynamoDB TransactWrite (트랜잭션)**

```go
// CommitSeatReservation 코드 (internal/repo/dynamodb.go:219-276)

var transactItems []types.TransactWriteItem

for _, seatID := range seatIDs {
    eventSeatID := fmt.Sprintf("%s#%s", eventID, seatID)
    
    transactItems = append(transactItems, types.TransactWriteItem{
        Update: &types.Update{
            TableName: "inventory_seats",
            Key: {"event_seat_id": eventSeatID},
            UpdateExpression: "SET status = :sold, reservation_id = :rid, updated_at = :now",
            
            // 🔒 핵심 조건: 좌석이 비어있거나(AVAILABLE) 또는 내 예약(HOLD)일 때만
            ConditionExpression: "(attribute_not_exists(reservation_id) AND status = :available) OR (reservation_id = :rid AND status = :hold)",
            
            ExpressionAttributeValues: {
                ":sold": "SOLD",
                ":available": "AVAILABLE",
                ":hold": "HOLD",
                ":rid": reservationID,
                ":now": time.Now(),
            },
        },
    })
}

// ⚠️ 모든 좌석이 조건을 만족해야만 전체 트랜잭션 성공
_, err := r.client.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
    TransactItems: transactItems,
})
```

**동작 원리:**

1. **ALL-OR-NOTHING 트랜잭션**
   - 예를 들어 A-12, A-13, A-14 세 좌석을 예약하려면:
   - **세 좌석 모두** 조건을 만족해야만 전체 예약 성공
   - **하나라도 실패하면** 전체 롤백 → 오버셀 불가능

2. **이중 조건 검사**
   ```
   조건 1: (attribute_not_exists(reservation_id) AND status = 'AVAILABLE')
   → 완전히 비어있는 좌석
   
   조건 2: (reservation_id = :rid AND status = 'HOLD')
   → 내가 이미 홀드(HOLD)한 좌석을 확정(SOLD)하는 경우
   ```

3. **동시 요청 시나리오**
   ```
   Initial State: A-12 좌석 (status=AVAILABLE, reservation_id=null)
   
   ┌─────────────────────────────────────────────────┐
   │  User A (rsv_123)        User B (rsv_456)       │
   │         │                         │             │
   │         ▼                         ▼             │
   │  TransactWrite A-12       TransactWrite A-12    │
   │  Condition: AVAILABLE ✅  Condition: AVAILABLE ⏳│
   │  Update: SOLD, rsv_123    (DynamoDB 대기)       │
   │         │                         │             │
   │         ✅ 성공                    ▼             │
   │                          Condition: SOLD ≠ AVAILABLE ❌
   │                          TransactionCanceledException
   └─────────────────────────────────────────────────┘
   ```
   - **DynamoDB는 트랜잭션 격리(Isolation)를 보장**
   - 먼저 커밋된 트랜잭션이 좌석을 SOLD로 변경
   - 다음 트랜잭션은 `status = AVAILABLE` 조건 실패

---

## 🔐 추가 안전장치

### 1. 멱등성 보장 (Idempotency)

```go
// CheckIdempotency 코드 (internal/repo/dynamodb.go:372-406)
func (r *Repository) CheckIdempotency(ctx context.Context, reservationID, method string) (*IdempotencyRecord, error) {
    // DynamoDB에서 이전 실행 결과 조회
    // 이미 처리된 요청이면 캐시된 응답 반환
}

func (r *Repository) SaveIdempotency(ctx context.Context, record *IdempotencyRecord) error {
    // TTL 300초 (5분) 설정하여 중복 방지
    record.TTL = time.Now().Add(5 * time.Minute).Unix()
}
```

**동작:**
- 같은 `reservation_id`로 여러 번 요청해도 **한 번만 실행**
- 네트워크 재시도, 클라이언트 중복 클릭 등에서 오버셀 방지

### 2. 버전 관리 (Versioning)

```go
// 모든 업데이트마다 version 필드 증가
updateExpr := "SET remaining = remaining - :qty, version = version + 1"

// 옵션: 낙관적 잠금 활성화 시
conditionExpr := "remaining >= :qty AND attribute_exists(version)"
```

**효과:**
- 동시 수정 감지 및 재시도 강제
- Lost Update 문제 방지

### 3. 트랜잭션 재시도 없음

```go
// 조건 실패 시 즉시 실패 반환 (재시도 X)
if err != nil {
    if err == ConditionalCheckFailedException {
        return ErrInsufficientInventory  // 재고 부족
    }
    if err == TransactionCanceledException {
        return ErrSeatAlreadyReserved    // 좌석 이미 예약됨
    }
}
```

**이유:**
- 재고가 부족한 상황에서 재시도는 무의미
- 클라이언트에게 명확한 실패 응답 (HTTP 409 Conflict)

---

## 📈 성능 vs 일관성 트레이드오프

### DynamoDB 읽기 일관성 설정

```go
// GetInventory - Strong Consistency 사용
input := &dynamodb.GetItemInput{
    TableName: "inventory",
    Key: {"event_id": eventID},
    ConsistentRead: aws.Bool(true),  // 🔒 강한 일관성
}
```

**선택 이유:**
- **Eventually Consistent Read (기본)**: 더 빠르지만 최신 데이터 보장 안 됨
- **Strongly Consistent Read (채택)**: 약간 느리지만 최신 재고 보장
- 오버셀 방지를 위해 **Strong Consistency 필수**

---

## 🧪 테스트 시나리오

### 1. 동시 요청 경합 테스트

```bash
# 100명이 동시에 마지막 1장 티켓 요청
for i in {1..100}; do
  grpcurl -d '{"event_id":"evt_001","qty":1,"reservation_id":"rsv_'$i'"}' \
    localhost:8080 inventory.v1.Inventory/CommitReservation &
done
wait

# 예상 결과: 1명만 성공, 99명은 INSUFFICIENT_INVENTORY
```

### 2. 좌석 더블 부킹 방지 테스트

```bash
# 두 사용자가 동일 좌석(A-12) 예약 시도
grpcurl -d '{"event_id":"evt_001","seat_ids":["A-12"],"reservation_id":"rsv_A"}' \
  localhost:8080 inventory.v1.Inventory/CommitReservation &

grpcurl -d '{"event_id":"evt_001","seat_ids":["A-12"],"reservation_id":"rsv_B"}' \
  localhost:8080 inventory.v1.Inventory/CommitReservation &

wait

# 예상 결과: 1명만 성공, 1명은 CONFLICT
```

### 3. 멱등성 테스트

```bash
# 동일 reservation_id로 3번 요청
for i in {1..3}; do
  grpcurl -d '{"event_id":"evt_001","qty":1,"reservation_id":"rsv_same_id"}' \
    localhost:8080 inventory.v1.Inventory/CommitReservation
done

# 예상 결과: 1번만 실행, 나머지는 캐시된 응답 (재고 1번만 차감)
```

---

## 📊 모니터링 메트릭

### Prometheus 메트릭으로 오버셀 방지 모니터링

```promql
# 재고 충돌 발생률 (조건 실패)
rate(inventory_conflicts_total[5m])

# 충돌 유형별 분석
sum by (type) (inventory_conflicts_total)
# type=quantity: 수량형 재고 부족
# type=seat: 좌석 충돌

# DynamoDB 조건부 업데이트 지연시간
histogram_quantile(0.95, dynamodb_operation_duration_seconds_bucket{operation="UpdateItem"})

# 트랜잭션 성공률
rate(inventory_commit_reservations_total{status="success"}[5m]) 
/ 
rate(inventory_commit_reservations_total[5m])
```

### 알림 규칙

```yaml
# Prometheus Alert
- alert: HighInventoryConflictRate
  expr: rate(inventory_conflicts_total[5m]) > 10
  annotations:
    summary: "높은 재고 충돌률 감지 ({{ $value }} conflicts/sec)"
    description: "동시 요청으로 인한 충돌이 급증했습니다. 재고 조정 필요."
```

---

## 🎓 핵심 요약

| 메커니즘 | 기술 | 효과 |
|---------|------|------|
| **수량형 재고** | DynamoDB Conditional Update | `remaining >= qty` 조건으로 원자적 차감 |
| **좌석형 재고** | DynamoDB TransactWrite | 모든 좌석 조건 만족 시에만 전체 커밋 |
| **멱등성 보장** | Idempotency Key + TTL | 중복 요청 방지 (5분 캐시) |
| **낙관적 잠금** | Version 필드 | 동시 수정 감지 및 재시도 |
| **강한 일관성** | ConsistentRead=true | 최신 재고 데이터 보장 |
| **트랜잭션 격리** | DynamoDB Serialization | 동시 요청 직렬화 처리 |

**결론: DynamoDB의 원자적 연산과 트랜잭션을 활용하여 데이터베이스 레벨에서 오버셀을 완전히 차단합니다.**

---

## 📚 참고 자료

- [AWS DynamoDB Conditional Writes](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/WorkingWithItems.html#WorkingWithItems.ConditionalUpdate)
- [DynamoDB Transactions](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/transaction-apis.html)
- [Optimistic Locking with Version Number](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBMapper.OptimisticLocking.html)

