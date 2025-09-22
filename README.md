# Inventory API

고성능 gRPC 기반 재고 관리 서비스로, 30k RPS 트래픽에서 **오버셀 0%**를 보장하는 Traffic Tacos MSA 플랫폼의 핵심 컴포넌트입니다.

## 🎯 핵심 특징

- **제로 오버셀**: DynamoDB 조건부 업데이트로 재고 충돌 방지
- **고성능**: P95 < 40ms 목표, 30k RPS 처리 가능
- **이중 모드**: 수량 기반 + 좌석 기반 재고 관리
- **완전한 관측성**: OpenTelemetry, Prometheus 통합
- **멱등성 보장**: reservation_id 기반 중복 처리 방지
- **중앙화된 프로토**: proto-contracts 리포지토리 통합

## 🏗️ 아키텍처

```
┌─────────────────┐    gRPC     ┌──────────────────┐    DynamoDB    ┌─────────────────┐
│ reservation-api │────────────▶│  inventory-api   │───────────────▶│   DynamoDB      │
│ (Kotlin/Spring) │             │   (Go/gRPC)      │                │ • inventory     │
└─────────────────┘             │                  │                │ • seats         │
                                │ • 재고 관리       │                │ • idempotency   │
                                │ • 동시성 제어     │                └─────────────────┘
                                │ • 멱등성 처리     │
                                └──────────────────┘
```

## 📁 프로젝트 구조

```
inventory-api/
├── cmd/inventory-api/          # 애플리케이션 진입점
├── internal/
│   ├── config/                 # 환경변수 설정
│   ├── server/                 # gRPC 서버 & 인터셉터
│   ├── service/                # 비즈니스 로직
│   ├── repo/                   # DynamoDB 접근 계층
│   └── observability/          # 로깅, 메트릭, 추적
├── tests/                      # 테스트 코드
├── .env.local                  # 로컬 환경 설정 (gitignore됨)
├── Makefile                    # 빌드 스크립트
└── Dockerfile                  # 컨테이너 이미지
```

> **프로토 파일**: proto-contracts 리포지토리에서 중앙 관리됩니다.

## 🚀 빠른 시작

### 사전 요구사항

- Go 1.23+
- AWS CLI (DynamoDB 접근용)
- Docker (컨테이너 실행용)
- grpcui (API 테스트용, 선택사항)

### 로컬 개발 설정

1. **저장소 클론 및 의존성 설치**
```bash
git clone https://github.com/traffictacos/inventory-api.git
cd inventory-api
go mod download
```

2. **환경변수 설정**
```bash
# .env.local 파일을 생성하고 실제 AWS 설정 입력
cp .env.local.example .env.local
# AWS Profile, DynamoDB 테이블명 등 설정
```

3. **빌드 & 실행**
```bash
make build-local
source .env.local && AWS_PROFILE=tacos ./bin/inventory-api
```

4. **grpcui로 API 테스트 (선택사항)**
```bash
# grpcui 설치
go install github.com/fullstorydev/grpcui/cmd/grpcui@latest

# 웹 인터페이스 실행
grpcui -plaintext localhost:50051
```

### Docker 실행

```bash
# 이미지 빌드
make docker-build

# 컨테이너 실행
docker run -p 50051:50051 -p 8081:8081 \
  --env-file .env.local \
  localhost:5000/inventory-api:latest
```

## 📊 gRPC API 사양

### 서비스 정의

proto-contracts 리포지토리에서 정의된 gRPC 서비스:

```protobuf
service InventoryService {
  rpc CheckAvailability(CheckAvailabilityRequest) returns (CheckAvailabilityResponse);
  rpc CommitReservation(CommitReservationRequest) returns (CommitReservationResponse);
  rpc ReleaseHold(ReleaseHoldRequest) returns (ReleaseHoldResponse);
}
```

### API 테스트 방법

#### grpcui 사용 (추천)
```bash
# 웹 인터페이스로 API 테스트
grpcui -plaintext localhost:50051
# 브라우저에서 표시된 URL 접속
```

#### grpcurl 사용

##### 1. CheckAvailability - 재고 확인
```bash
grpcurl -plaintext -d '{
  "event_id": "test-event-1",
  "quantity": 2
}' localhost:50051 reservation.v1.InventoryService/CheckAvailability
```

##### 2. CommitReservation - 예약 확정
```bash
grpcurl -plaintext -d '{
  "reservation_id": "rsv_abc123",
  "event_id": "test-event-1",
  "quantity": 2,
  "payment_intent_id": "pay_xyz789"
}' localhost:50051 reservation.v1.InventoryService/CommitReservation
```

##### 3. ReleaseHold - 홀드 해제
```bash
grpcurl -plaintext -d '{
  "reservation_id": "rsv_abc123",
  "event_id": "test-event-1",
  "quantity": 2
}' localhost:50051 reservation.v1.InventoryService/ReleaseHold
```

## 🗄️ 데이터베이스 스키마

### DynamoDB 테이블

#### 1. inventory 테이블 (수량형 재고)
```javascript
{
  "event_id": "evt_2025_1001",        // PK
  "remaining": 1234,                  // 잔여 수량
  "version": 42,                      // 낙관적 잠금
  "updated_at": "2024-01-01T12:00:00Z",
  "section_remaining": {              // 선택사항
    "A": 120,
    "B": 80
  }
}
```

#### 2. inventory_seats 테이블 (좌석형 재고)
```javascript
{
  "event_seat_id": "evt_2025_1001#A-12",  // PK (event_id#seat_id)
  "status": "AVAILABLE",                  // AVAILABLE|HOLD|SOLD
  "reservation_id": "rsv_abc123",         // 예약 ID (옵션)
  "updated_at": "2024-01-01T12:00:00Z"
}
```

#### 3. idempotency 테이블 (멱등성 관리)
```javascript
{
  "reservation_id": "rsv_abc123",         // PK
  "method": "CommitReservation",          // SK
  "response": "...",                      // 캐시된 응답
  "ttl": 1704096000,                      // TTL (5분)
  "created_at": "2024-01-01T12:00:00Z"
}
```

## ⚙️ 환경 설정

### 필수 환경변수

```bash
# 서버 설정
GRPC_PORT=50051
LOG_LEVEL=debug

# AWS 설정
AWS_REGION=ap-northeast-2
AWS_PROFILE=tacos

# DynamoDB 테이블 (실제 AWS 리소스)
DYNAMODB_TABLE_INVENTORY=ticket-tickets
DYNAMODB_TABLE_SEATS=ticket-tickets
DYNAMODB_TABLE_IDEMPOTENCY=ticket-reservation-idempotency

# 성능 설정
INVENTORY_MODE=quantity              # quantity|seat
USE_OPTIMISTIC_LOCKING=true
IDEMPOTENCY_TTL_SECONDS=300

# 관측성 설정
OTLP_ENDPOINT=localhost:4317
ENABLE_TRACING=true
ENABLE_METRICS=true
```

## 🧪 테스트

### 단위 테스트
```bash
make test
```

### 통합 테스트 (LocalStack 필요)
```bash
make test-integration
```

### 부하 테스트
```bash
# ghz 사용
make load-test-ghz

# 또는 직접 실행
./tools/load-test.sh localhost 8080 800 30s
```

## 📈 성능 목표

| 메트릭 | 목표 | 설명 |
|--------|------|------|
| **처리량** | 30k RPS | 시스템 전체 목표 |
| **P95 지연시간** | < 40ms | gRPC 메서드 응답 시간 |
| **오류율** | < 0.5% | 재고 충돌 제외 |
| **가용성** | 99.9% | SLA 목표 |

## 📊 모니터링

### 메트릭 엔드포인트
- **Prometheus**: `http://localhost:8081/metrics`
- **Health Check**: `http://localhost:8081/health`

### 주요 메트릭

```promql
# gRPC 성능
grpc_server_handling_seconds_bucket{method="CheckAvailability"}

# 재고 충돌
inventory_conflicts_total{type="quantity",event_id="evt_123"}

# DynamoDB 성능
dynamodb_latency_seconds_bucket{operation="UpdateItem",table="inventory"}

# 멱등성 히트율
idempotent_hit_total{method="CommitReservation"}
```

## 🐛 문제 해결

### 일반적인 문제

#### 1. 재고 충돌 오류
```
Error: inventory conflict: seats unavailable or insufficient quantity
```
**해결**: 정상적인 동작입니다. 동시 요청으로 인한 재고 부족을 의미합니다.

#### 2. DynamoDB 연결 실패
```
Error: failed to load AWS configuration
```
**해결**: AWS 자격 증명을 확인하세요.
```bash
aws configure list-profiles
export AWS_PROFILE=tacos
```

#### 3. gRPC 연결 오류
```
Error: connection refused
```
**해결**: 서비스가 실행 중인지 확인하세요.
```bash
./health_check.sh check
```

### 로그 확인

```bash
# 구조화된 JSON 로그
tail -f logs/inventory-api.log | jq '.'

# 특정 추적 ID 필터링
grep "trace_id=abc123" logs/inventory-api.log
```

## 🚀 배포

### 로컬 테스트
```bash
make ci                # 전체 CI 파이프라인
```

### 프로덕션 빌드
```bash
make docker-build      # ARM64/AMD64 멀티 아키텍처
docker push registry.com/inventory-api:v1.0.0
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
        image: registry.com/inventory-api:v1.0.0
        ports:
        - containerPort: 8080
        env:
        - name: AWS_REGION
          value: "ap-northeast-2"
        resources:
          requests:
            cpu: 200m
            memory: 256Mi
          limits:
            cpu: 500m
            memory: 512Mi
```

## 🛠️ 개발 워크플로

### 1. 기능 개발
```bash
# 브랜치 생성
git checkout -b feature/new-feature

# 개발 및 테스트
make ci

# 코드 검토
make lint
```

### 2. 프로토 변경
```bash
# 프로토 파일 수정
vim proto/inventory.proto

# 코드 재생성
make generate

# 호환성 확인
buf breaking --against '.git#branch=main'
```

### 3. 성능 테스트
```bash
# 로컬 성능 테스트
make perf-test

# 부하 테스트
./tools/load-test.sh localhost 8080 1000 60s
```

## 📝 라이센스

이 프로젝트는 Traffic Tacos 팀의 내부 프로젝트입니다.

## 🤝 기여

1. 이슈 생성 또는 기능 요청
2. 브랜치 생성 (`git checkout -b feature/amazing-feature`)
3. 변경사항 커밋 (`git commit -m 'Add amazing feature'`)
4. 브랜치 푸시 (`git push origin feature/amazing-feature`)
5. Pull Request 생성

## 📞 지원

- **이슈 트래킹**: GitHub Issues
- **문서**: [API 명세서](./docs/api-spec.md)
- **모니터링**: Grafana 대시보드
- **팀 연락**: #traffic-tacos-dev 슬랙 채널