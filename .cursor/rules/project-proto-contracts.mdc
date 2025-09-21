# Traffic Tacos Proto Contracts

## 프로젝트 개요

Traffic Tacos MSA 플랫폼의 중앙화된 gRPC proto contracts 저장소입니다. 30k RPS 티켓 예약 시스템을 위한 서비스 간 통신 계약을 정의합니다.

## 아키텍처

```
Traffic Tacos MSA Platform (30k RPS)
├── Gateway Layer (트래픽 제어)
│   ├── QueueService - 대기열 관리 및 admission control
│   ├── GatewayService - BFF 및 인증/인가
│   ├── WebSocketService - 실시간 업데이트 스트리밍
│   └── WebhookService - 외부 이벤트 수신
├── Business Layer (핵심 비즈니스 로직)
│   ├── ReservationService - 예약 관리 (60초 hold)
│   ├── InventoryService - 재고 관리 (zero oversell)
│   └── PaymentService - 결제 시뮬레이션
└── Background Layer (백그라운드 처리)
    ├── WorkerService - 만료/결제 이벤트 처리
    └── AdminService - 시스템 모니터링
```

## 서비스 목록

### Gateway Services (gateway.v1)
- **QueueService**: 30k RPS 트래픽 관리, admission control
- **GatewayService**: BFF, 인증/인가, 요청 라우팅
- **WebSocketService**: 실시간 대기열/예약/결제 상태 업데이트
- **WebhookService**: 결제/만료 webhook 이벤트 수신

### Reservation Services (reservation.v1)
- **ReservationService**: 예약 생성/확인/취소 (60초 hold 메커니즘)
- **InventoryService**: 좌석 재고 관리 (DynamoDB conditional updates)
- **WorkerService**: 백그라운드 작업 (만료/결제 처리, KEDA auto-scaling)

### Payment Services (payment.v1)
- **PaymentService**: 결제 시뮬레이션, webhook 콜백

### Common Services (common.v1)
- **AdminService**: 시스템 헬스체크, 메트릭, 운영 도구

## Go Module 사용법

### 설치
```bash
go get github.com/traffic-tacos/proto-contracts@latest
```

### 임포트
```go
import (
    gatewaypb "github.com/traffic-tacos/proto-contracts/gen/go/gateway/v1"
    reservationpb "github.com/traffic-tacos/proto-contracts/gen/go/reservation/v1"
    paymentpb "github.com/traffic-tacos/proto-contracts/gen/go/payment/v1"
    commonpb "github.com/traffic-tacos/proto-contracts/gen/go/common/v1"
)
```

### 클라이언트 사용 예제
```go
// Queue 클라이언트
queueClient := gatewaypb.NewQueueServiceClient(conn)
joinResp, err := queueClient.JoinQueue(ctx, &gatewaypb.JoinQueueRequest{
    EventId:   "evt_2025_concert",
    UserId:    "user_12345",
    SessionId: "session_abcdef",
})

// Reservation 클라이언트
reservationClient := reservationpb.NewReservationServiceClient(conn)
createResp, err := reservationClient.CreateReservation(ctx, &reservationpb.CreateReservationRequest{
    EventId:          "evt_2025_concert",
    SeatIds:          []string{"A-12", "A-13"},
    Quantity:         2,
    ReservationToken: joinResp.ReservationToken,
})
```

### 서버 구현 예제
```go
type queueServer struct {
    gatewaypb.UnimplementedQueueServiceServer
}

func (s *queueServer) JoinQueue(ctx context.Context, req *gatewaypb.JoinQueueRequest) (*gatewaypb.JoinQueueResponse, error) {
    // 대기열 로직 구현
    return &gatewaypb.JoinQueueResponse{
        WaitingToken: generateToken(),
        PositionHint: calculatePosition(req.EventId),
        EtaSeconds:   estimateWaitTime(req.EventId),
        Status:       gatewaypb.QueueStatus_QUEUE_STATUS_WAITING,
    }, nil
}
```

## Kotlin/Spring 사용법

### Gradle 의존성
```kotlin
dependencies {
    implementation("com.traffic-tacos:proto-contracts:1.0.0")
    implementation("io.grpc:grpc-kotlin-stub:1.3.0")
    implementation("io.grpc:grpc-protobuf:1.58.0")
}
```

### 클라이언트 사용 (Kotlin)
```kotlin
@Component
class QueueGrpcClient(
    @Qualifier("queueServiceStub")
    private val queueStub: QueueServiceGrpcKt.QueueServiceCoroutineStub
) {
    suspend fun joinQueue(eventId: String, userId: String): JoinQueueResponse {
        val request = JoinQueueRequest.newBuilder()
            .setEventId(eventId)
            .setUserId(userId)
            .setSessionId(generateSessionId())
            .build()

        return queueStub.joinQueue(request)
    }
}
```

### 서버 구현 (Kotlin)
```kotlin
@GrpcService
class QueueGrpcService : QueueServiceGrpcKt.QueueServiceCoroutineImplBase() {

    override suspend fun joinQueue(request: JoinQueueRequest): JoinQueueResponse {
        return JoinQueueResponse.newBuilder()
            .setWaitingToken(generateToken())
            .setPositionHint(calculatePosition(request.eventId))
            .setEtaSeconds(estimateWaitTime(request.eventId))
            .setStatus(QueueStatus.QUEUE_STATUS_WAITING)
            .build()
    }
}
```

### Spring Boot 구성
```kotlin
@Configuration
class GrpcClientConfig {

    @Bean
    fun queueServiceChannel(): ManagedChannel {
        return ManagedChannelBuilder
            .forAddress("queue-service", 9090)
            .usePlaintext()
            .build()
    }

    @Bean
    fun queueServiceStub(channel: ManagedChannel): QueueServiceGrpcKt.QueueServiceCoroutineStub {
        return QueueServiceGrpcKt.QueueServiceCoroutineStub(channel)
    }
}
```

## 핵심 비즈니스 플로우

### 1. 대기열 → 예약 플로우
```
1. JoinQueue (30k RPS 처리)
2. StreamQueueUpdates (실시간 위치 업데이트)
3. RequestAdmission (admission ready시)
4. CreateReservation (60초 hold)
5. ProcessPayment (결제 처리)
6. ConfirmReservation (확정) 또는 ExpireReservation (만료)
```

### 2. 실시간 업데이트 플로우
```
WebSocket 연결 → StreamQueueUpdates/StreamReservationUpdates →
클라이언트 UI 업데이트 (위치, 상태, 만료 카운트다운)
```

### 3. 백그라운드 처리 플로우
```
EventBridge Timer → SQS → Worker (KEDA scaling) →
ProcessReservationExpiry/ProcessPaymentResult
```

## 에러 처리

모든 응답에는 `common.v1.Error` 필드가 포함됩니다:
```protobuf
message Error {
  ErrorCode code = 1;
  string message = 2;
  string trace_id = 3;
  repeated ErrorDetail details = 4;
}
```

### 공통 에러 코드
- `QUEUE_FULL`: 대기열 포화상태
- `INVALID_TOKEN`: 잘못된 토큰
- `RESERVATION_EXPIRED`: 예약 만료
- `PAYMENT_FAILED`: 결제 실패
- `SEAT_UNAVAILABLE`: 좌석 매진

## 개발 가이드라인

### Proto 파일 수정 시
1. `buf lint` 실행하여 린트 체크
2. `buf breaking` 실행하여 호환성 체크
3. `make generate` 실행하여 코드 생성
4. `make test` 실행하여 테스트

### 버전 관리
- 모든 proto 파일은 v1 네임스페이스 사용
- Breaking change 시 v2 네임스페이스 생성
- Git 태그를 통한 semantic versioning

### 성능 고려사항
- **QueueService**: 30k RPS 처리 가능하도록 설계
- **ReservationService**: 60초 hold 메커니즘으로 동시성 제어
- **InventoryService**: DynamoDB conditional update로 zero oversell 보장
- **WorkerService**: KEDA auto-scaling으로 백그라운드 처리

## 문서 및 예제

- `docs/API_SPECIFICATION.md`: 전체 API 상세 스펙
- `docs/INTEGRATION_GUIDE.md`: 서비스별 연동 가이드
- `docs/MIGRATION_GUIDE.md`: 기존 서비스 마이그레이션 가이드
- `examples/go/`: Go 클라이언트 예제
- `examples/kotlin/`: Kotlin 서비스 예제
- `examples/typescript/`: TypeScript 프론트엔드 예제

## 관련 저장소

- **gateway-api**: Queue, Gateway, WebSocket, Webhook 서비스 구현
- **reservation-api**: Reservation, Inventory 서비스 구현
- **payment-sim-api**: Payment 시뮬레이션 서비스
- **reservation-worker**: 백그라운드 Worker 서비스
- **reservation-web**: React 프론트엔드 (WebSocket 연동)

## 모니터링 및 운영

AdminService를 통해 다음 기능 제공:
- 시스템 헬스체크
- 실시간 메트릭 수집
- 로그 조회 및 검색
- 긴급 운영 액션 (admission 중단, circuit breaker 등)

## 보안 고려사항

- gRPC TLS 암호화 사용
- JWT 토큰 기반 인증
- Rate limiting 및 DDoS 방어
- Webhook HMAC 서명 검증
- 민감 정보 로깅 금지