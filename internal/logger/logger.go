package logger

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	log     *slog.Logger
	logOnce sync.Once
)

// Level 定义日志级别
type Level string

const (
	LevelDebug Level = "debug"
	LevelInfo  Level = "info"
	LevelWarn  Level = "warn"
	LevelError Level = "error"
)

// Init 初始化全局日志器
func Init(level string, format string, output string) {
	logOnce.Do(func() {
		var logLevel slog.Level
		switch level {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			logLevel = slog.LevelInfo
		}

		var handler slog.Handler
		if format == "json" {
			handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
				ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
					// 保留时间戳但简化键名
					if a.Key == slog.TimeKey {
						return slog.Attr{Key: "time", Value: a.Value}
					}
					if a.Key == slog.LevelKey {
						return slog.Attr{Key: "level", Value: a.Value}
					}
					if a.Key == slog.MessageKey {
						return slog.Attr{Key: "msg", Value: a.Value}
					}
					return a
				},
			})
		} else {
			handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
				Level: logLevel,
			})
		}

		log = slog.New(handler)
		slog.SetDefault(log)
	})
}

// Get 返回全局日志实例
func Get() *slog.Logger {
	if log == nil {
		Init("info", "json", "stdout")
	}
	return log
}

// ContextKey 日志上下文键类型
type ContextKey string

const (
	// ContextKeyTraceID 链路追踪 ID
	ContextKeyTraceID ContextKey = "trace_id"
	// ContextKeyUserID 用户 ID
	ContextKeyUserID ContextKey = "user_id"
	// ContextKeyConnID 连接 ID
	ContextKeyConnID ContextKey = "conn_id"
	// ContextKeyConversationID 会话 ID
	ContextKeyConversationID ContextKey = "conversation_id"
	// ContextKeyMsgID 消息 ID
	ContextKeyMsgID ContextKey = "msg_id"
	// ContextKeyInstanceID 实例 ID
	ContextKeyInstanceID ContextKey = "instance_id"
)

// ContextLogger 带有上下文的日志器
type ContextLogger struct {
	logger *slog.Logger
	ctx    context.Context
}

// FromContext 从 context 创建带上下文的日志器
func FromContext(ctx context.Context) *ContextLogger {
	l := Get()
	
	// 从 context 提取上下文信息
	attrs := []any{}
	
	if traceID, ok := ctx.Value(ContextKeyTraceID).(string); ok && traceID != "" {
		attrs = append(attrs, "trace_id", traceID)
	}
	if userID, ok := ctx.Value(ContextKeyUserID).(string); ok && userID != "" {
		attrs = append(attrs, "user_id", userID)
	}
	if connID, ok := ctx.Value(ContextKeyConnID).(string); ok && connID != "" {
		attrs = append(attrs, "conn_id", connID)
	}
	if conversationID, ok := ctx.Value(ContextKeyConversationID).(string); ok && conversationID != "" {
		attrs = append(attrs, "conversation_id", conversationID)
	}
	if msgID, ok := ctx.Value(ContextKeyMsgID).(string); ok && msgID != "" {
		attrs = append(attrs, "msg_id", msgID)
	}
	if instanceID, ok := ctx.Value(ContextKeyInstanceID).(string); ok && instanceID != "" {
		attrs = append(attrs, "instance_id", instanceID)
	}
	
	var logger *slog.Logger
	if len(attrs) > 0 {
		logger = l.With(attrs...)
	} else {
		logger = l
	}
	
	return &ContextLogger{
		logger: logger,
		ctx:    ctx,
	}
}

// With 创建新的 ContextLogger 并添加额外上下文
func (cl *ContextLogger) With(args ...any) *ContextLogger {
	return &ContextLogger{
		logger: cl.logger.With(args...),
		ctx:    cl.ctx,
	}
}

// Debug 调试级别日志
func (cl *ContextLogger) Debug(msg string, args ...any) {
	cl.logger.Debug(msg, args...)
}

// Info 信息级别日志
func (cl *ContextLogger) Info(msg string, args ...any) {
	cl.logger.Info(msg, args...)
}

// Warn 警告级别日志
func (cl *ContextLogger) Warn(msg string, args ...any) {
	cl.logger.Warn(msg, args...)
}

// Error 错误级别日志
func (cl *ContextLogger) Error(msg string, args ...any) {
	cl.logger.Error(msg, args...)
}

// WithTraceID 添加链路追踪 ID
func WithTraceID(ctx context.Context, traceID string) context.Context {
	if traceID == "" {
		traceID = uuid.New().String()
	}
	return context.WithValue(ctx, ContextKeyTraceID, traceID)
}

// WithUserID 添加用户 ID
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, ContextKeyUserID, userID)
}

// WithConnID 添加连接 ID
func WithConnID(ctx context.Context, connID string) context.Context {
	return context.WithValue(ctx, ContextKeyConnID, connID)
}

// WithConversationID 添加会话 ID
func WithConversationID(ctx context.Context, conversationID string) context.Context {
	return context.WithValue(ctx, ContextKeyConversationID, conversationID)
}

// WithMsgID 添加消息 ID
func WithMsgID(ctx context.Context, msgID string) context.Context {
	return context.WithValue(ctx, ContextKeyMsgID, msgID)
}

// WithInstanceID 添加实例 ID
func WithInstanceID(ctx context.Context, instanceID string) context.Context {
	return context.WithValue(ctx, ContextKeyInstanceID, instanceID)
}

// GetTraceID 从 context 获取链路追踪 ID
func GetTraceID(ctx context.Context) string {
	if traceID, ok := ctx.Value(ContextKeyTraceID).(string); ok {
		return traceID
	}
	return ""
}

// ============ 兼容旧API ============

// Debug 记录调试日志
func Debug(msg string, args ...any) {
	Get().Debug(msg, args...)
}

// Info 记录信息日志
func Info(msg string, args ...any) {
	Get().Info(msg, args...)
}

// Warn 记录警告日志
func Warn(msg string, args ...any) {
	Get().Warn(msg, args...)
}

// Error 记录错误日志
func Error(msg string, args ...any) {
	Get().Error(msg, args...)
}

// With 返回带有属性的日志器
func With(args ...any) *slog.Logger {
	return Get().With(args...)
}

// StructuredLog 结构化日志辅助函数
// 用于记录带有标准字段的日志，便于日志分析和追踪
type StructuredLog struct {
	TraceID        string `json:"trace_id,omitempty"`
	UserID         string `json:"user_id,omitempty"`
	ConnID         string `json:"conn_id,omitempty"`
	ConversationID string `json:"conversation_id,omitempty"`
	MsgID          string `json:"msg_id,omitempty"`
	InstanceID     string `json:"instance_id,omitempty"`
	Operation      string `json:"operation,omitempty"`
	Duration       int64  `json:"duration_ms,omitempty"`
	Error          string `json:"error,omitempty"`
}

// LogStructured 记录结构化日志
func LogStructured(level Level, op string, duration time.Duration, err error, fields StructuredLog) {
	l := Get()
	
	attrs := []any{
		"operation", op,
	}
	
	if fields.TraceID != "" {
		attrs = append(attrs, "trace_id", fields.TraceID)
	}
	if fields.UserID != "" {
		attrs = append(attrs, "user_id", fields.UserID)
	}
	if fields.ConnID != "" {
		attrs = append(attrs, "conn_id", fields.ConnID)
	}
	if fields.ConversationID != "" {
		attrs = append(attrs, "conversation_id", fields.ConversationID)
	}
	if fields.MsgID != "" {
		attrs = append(attrs, "msg_id", fields.MsgID)
	}
	if fields.InstanceID != "" {
		attrs = append(attrs, "instance_id", fields.InstanceID)
	}
	if duration > 0 {
		attrs = append(attrs, "duration_ms", duration.Milliseconds())
	}
	if err != nil {
		attrs = append(attrs, "error", err.Error())
	}
	
	switch level {
	case LevelDebug:
		l.Debug("structured_log", attrs...)
	case LevelInfo:
		l.Info("structured_log", attrs...)
	case LevelWarn:
		l.Warn("structured_log", attrs...)
	case LevelError:
		l.Error("structured_log", attrs...)
	}
}
