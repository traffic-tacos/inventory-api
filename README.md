# Inventory API

Traffic Tacos 티켓 예매 플랫폼의 **재고 관리 서비스**입니다. gRPC 기반 고성능 서비스로, **오버셀 0%** 보장과 **30k RPS 트래픽** 처리를 목표로 합니다.

## 🎯 핵심 기능

- **재고 관리**: 수량형/좌석형 인벤토리 모두 지원
- **오버셀 방지**: DynamoDB 조건부 업데이트 + TransactWrite로 0% 오버셀 보장
- **멱등성**: reservation_id 기반 중복 요청 처리
- **고성능**: P95 < 40ms, 500-800 RPS 처리 목표
- **관측 가능성**: OpenTelemetry 트레이싱 + Prometheus 메트릭

## 🏗️ 아키텍처

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  API Gateway    │────│ Reservation API │────│  Inventory API  │
│                 │    │   (Spring)      │    │   (Go/gRPC)     │
└─────────────────┘    └─────────────────┘    └─────────────────┘
                                              │                 │
                                              │ • CheckAvailability │
                                              │ • CommitReservation│
                                              │ • ReleaseHold      │
                                              └─────────────────┘
                                                         │
                                                         ▼
                                              ┌─────────────────┐
                                              │   DynamoDB      │
                                              │ • inventory     │
                                              │ • inventory_seats│
                                              │ • idempotency    │
                                              └─────────────────┘
```

## 🚀 빠른 시작

### 환경 설정

```bash
# 환경변수 설정
export AWS_REGION=ap-northeast-2
export DDB_TABLE_INVENTORY=inventory
export DDB_TABLE_SEATS=inventory_seats
export GRPC_PORT=8080
export OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4317
```

### 빌드 및 실행

```bash
# 빌드
make build

# 실행
./inventory-api

# 또는 Docker로 실행
make docker-run
```

### gRPC 호출 테스트

```bash
# 재고 확인
grpcurl -plaintext localhost:8080 inventory.v1.Inventory/CheckAvailability

# 예약 확정
grpcurl -plaintext -d '{"reservation_id": "rsv_123", "event_id": "evt_456", "qty": 2}' \
  localhost:8080 inventory.v1.Inventory/CommitReservation
```

## 📊 API 명세

### CheckAvailability
재고 가용성 확인 (읽기 전용)

```protobuf
rpc CheckAvailability(CheckReq) returns (CheckRes);
```

**요청:**
```json
{
  "event_id": "evt_2025_1001",
  "qty": 2,
  "seat_ids": [{"seat_id": "A-12"}]
}
```

**응답:**
```json
{
  "available": true,
  "unavailable_seats": []
}
```

### CommitReservation
예약 확정 (재고 감소, 오버셀 0% 보장)

```protobuf
rpc CommitReservation(CommitReq) returns (CommitRes);
```

**요청:**
```json
{
  "reservation_id": "rsv_abc123",
  "event_id": "evt_2025_1001",
  "qty": 2,
  "seat_ids": [{"seat_id": "A-12"}, {"seat_id": "A-13"}],
  "payment_intent_id": "pay_xyz789"
}
```

**응답:**
```json
{
  "order_id": "ord_xyz789",
  "status": "CONFIRMED"
}
```

### ReleaseHold
홀드 해제 (멱등성 보장)

```protobuf
rpc ReleaseHold(ReleaseReq) returns (ReleaseRes);
```

## 💾 데이터 모델

### Inventory 테이블 (수량형)
```javascript
{
  event_id: "evt_2025_1001",  // PK
  remaining: 8500,
  version: 42,               // 낙관적 잠금
  total_seats: 10000,
  updated_at: "2024-01-01T12:00:00Z"
}
```

### Inventory Seats 테이블 (좌석형)
```javascript
{
  event_id: "evt_2025_1001",  // PK
  seat_id: "A-12",           // SK
  status: "AVAILABLE",       // AVAILABLE | HOLD | SOLD
  reservation_id: null,
  updated_at: "2024-01-01T12:00:00Z"
}
```

## ⚙️ 환경변수

| 변수 | 기본값 | 설명 |
|------|--------|------|
| `GRPC_PORT` | 8080 | gRPC 서버 포트 |
| `AWS_REGION` | ap-northeast-2 | AWS 리전 |
| `DDB_TABLE_INVENTORY` | inventory | 인벤토리 테이블명 |
| `DDB_TABLE_SEATS` | inventory_seats | 좌석 테이블명 |
| `IDEMPOTENCY_TTL_SECONDS` | 300 | 멱등성 캐시 TTL |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | http://otel-collector:4317 | OTLP 엔드포인트 |
| `LOG_LEVEL` | info | 로그 레벨 |

## 📈 모니터링

### 메트릭
- `grpc_request_duration_seconds` - gRPC 요청 처리 시간
- `inventory_commit_reservations_total` - 예약 확정 수
- `inventory_conflicts_total` - 충돌 발생 수
- `dynamodb_operation_duration_seconds` - DynamoDB 작업 시간

### 헬스체크
```bash
curl http://localhost:9090/metrics
```

## 🧪 테스트

### 단위 테스트
```bash
make test
```

### 통합 테스트 (LocalStack)
```bash
# LocalStack 실행
docker run -d --name localstack -p 4566:4566 localstack/localstack

# 테스트 실행
make test-integration
```

### 부하 테스트
```bash
# ghz로 부하 테스트
make load-test-ghz
```

## 🚀 배포

### Docker 빌드
```bash
make docker-build
```

### Kubernetes 배포
```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inventory-api
spec:
  replicas: 3
  template:
    spec:
      containers:
      - name: inventory-api
        image: inventory-api:latest
        ports:
        - containerPort: 8080
        env:
        - name: AWS_REGION
          value: "ap-northeast-2"
        - name: DDB_TABLE_INVENTORY
          value: "inventory"
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "500m"
```

## 🎯 성능 목표

- **P95 지연시간**: < 40ms
- **처리량**: 500-800 RPS (CommitReservation)
- **오류율**: < 0.5%
- **가용성**: 99.9%

## 🔧 개발

### 코드 생성
```bash
# protobuf 코드 생성
make generate
```

### 포맷팅 및 린트
```bash
make format
make lint
```

### 디버깅
```bash
# 리플렉션 활성화된 서버 실행
./inventory-api

# grpcui로 API 탐색
grpcui -plaintext localhost:8080
```

## 🤝 기여

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📝 라이선스

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

**Traffic Tacos 팀**
