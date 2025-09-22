package observability

import (
	"context"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type Logger struct {
	*zap.Logger
}

func NewLogger(level string) (*Logger, error) {
	config := zap.NewProductionConfig()
	config.Encoding = "json"

	// Set log level
	switch level {
	case "debug":
		config.Level = zap.NewAtomicLevelAt(zapcore.DebugLevel)
	case "info":
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	case "warn":
		config.Level = zap.NewAtomicLevelAt(zapcore.WarnLevel)
	case "error":
		config.Level = zap.NewAtomicLevelAt(zapcore.ErrorLevel)
	default:
		config.Level = zap.NewAtomicLevelAt(zapcore.InfoLevel)
	}

	// Configure encoder
	config.EncoderConfig.TimeKey = "ts"
	config.EncoderConfig.LevelKey = "level"
	config.EncoderConfig.NameKey = "logger"
	config.EncoderConfig.CallerKey = "caller"
	config.EncoderConfig.MessageKey = "msg"
	config.EncoderConfig.StacktraceKey = "stacktrace"
	config.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	config.EncoderConfig.EncodeLevel = zapcore.LowercaseLevelEncoder
	config.EncoderConfig.EncodeCaller = zapcore.ShortCallerEncoder

	logger, err := config.Build()
	if err != nil {
		return nil, err
	}

	return &Logger{Logger: logger}, nil
}

func (l *Logger) WithContext(ctx context.Context) *Logger {
	// Extract trace ID from context if available
	if traceID := getTraceIDFromContext(ctx); traceID != "" {
		return &Logger{Logger: l.Logger.With(zap.String("trace_id", traceID))}
	}
	return l
}

func (l *Logger) WithFields(fields ...zap.Field) *Logger {
	return &Logger{Logger: l.Logger.With(fields...)}
}

type Field struct {
	Key   string
	Value interface{}
}

func StringField(key, value string) Field {
	return Field{Key: key, Value: value}
}

func Int32Field(key string, value int32) Field {
	return Field{Key: key, Value: value}
}

func IntField(key string, value int) Field {
	return Field{Key: key, Value: value}
}

func ErrorField(err error) Field {
	return Field{Key: "error", Value: err.Error()}
}

func (l *Logger) Info(msg string, fields ...Field) {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	l.Logger.Info(msg, zapFields...)
}

func (l *Logger) Error(msg string, fields ...Field) {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	l.Logger.Error(msg, zapFields...)
}

func (l *Logger) Warn(msg string, fields ...Field) {
	zapFields := make([]zap.Field, len(fields))
	for i, field := range fields {
		zapFields[i] = zap.Any(field.Key, field.Value)
	}
	l.Logger.Warn(msg, zapFields...)
}

func (l *Logger) LogGRPCRequest(ctx context.Context, method string, req interface{}) {
	l.WithContext(ctx).Info("gRPC request started",
		StringField("method", method),
		Field{Key: "request", Value: req},
	)
}

func (l *Logger) LogGRPCResponse(ctx context.Context, method string, resp interface{}, err error, latencyMs int64) {
	fields := []Field{
		StringField("method", method),
		Field{Key: "latency_ms", Value: latencyMs},
	}

	if err != nil {
		fields = append(fields, ErrorField(err))
		l.WithContext(ctx).Error("gRPC request failed", fields...)
	} else {
		fields = append(fields, Field{Key: "response", Value: resp})
		l.WithContext(ctx).Info("gRPC request completed", fields...)
	}
}

func (l *Logger) LogDynamoDBOperation(ctx context.Context, operation string, tableName string, latencyMs int64, rcu, wcu int64, err error) {
	fields := []Field{
		StringField("operation", operation),
		StringField("table_name", tableName),
		Field{Key: "latency_ms", Value: latencyMs},
		Field{Key: "ddb_rcu", Value: rcu},
		Field{Key: "ddb_wcu", Value: wcu},
	}

	if err != nil {
		fields = append(fields, ErrorField(err))
		l.WithContext(ctx).Error("DynamoDB operation failed", fields...)
	} else {
		l.WithContext(ctx).Info("DynamoDB operation completed", fields...)
	}
}

func getTraceIDFromContext(ctx context.Context) string {
	// TODO: Extract trace ID from OpenTelemetry context
	// This is a placeholder implementation
	return ""
}