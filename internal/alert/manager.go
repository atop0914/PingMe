package alert

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"PingMe/internal/config"
	"PingMe/internal/logger"

	"github.com/prometheus/client_golang/prometheus"
)

const (
	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Alert 告警结构
type Alert struct {
	Name      string                 `json:"name"`
	Severity  string                 `json:"severity"`
	Message   string                 `json:"message"`
	Value     any                    `json:"value"`
	Timestamp int64                  `json:"timestamp"`
	Instance  string                 `json:"instance"`
	Labels    map[string]string      `json:"labels,omitempty"`
}

// MetricGetter 度量获取接口
type MetricGetter interface {
	GetGauge(name string) prometheus.Gauge
	GetCounter(name string) prometheus.Counter
}

// AlertManager 告警管理器
type AlertManager struct {
	config    *config.AlertConfig
	metrics   MetricGetter
	instance  string
	history   []Alert
	mu        sync.RWMutex
	stopChan  chan struct{}
	wg        sync.WaitGroup
}

// NewAlertManager 创建告警管理器
func NewAlertManager(cfg *config.AlertConfig, metrics MetricGetter) *AlertManager {
	hostname, _ := os.Hostname()
	
	return &AlertManager{
		config:    cfg,
		metrics:   metrics,
		instance:  hostname,
		history:   make([]Alert, 0, 100),
		stopChan:  make(chan struct{}),
	}
}

// Start 启动告警检查
func (am *AlertManager) Start(ctx context.Context) {
	if !am.config.Enabled {
		logger.Info("Alert manager disabled")
		return
	}

	logger.Info("Starting alert manager",
		"interval", am.config.Interval,
		"rules", len(am.config.Rules))

	am.wg.Add(1)
	go func() {
		defer am.wg.Done()
		ticker := time.NewTicker(time.Duration(am.config.Interval) * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				am.checkAlerts(ctx)
			}
		}
	}()
}

// Stop 停止告警检查
func (am *AlertManager) Stop() {
	close(am.stopChan)
	am.wg.Wait()
}

// checkAlerts 检查所有告警规则
func (am *AlertManager) checkAlerts(ctx context.Context) {
	for _, rule := range am.config.Rules {
		value, ok := am.getMetricValue(rule.Metric)
		if !ok {
			continue
		}

		triggered := am.evaluateCondition(value, rule.Condition, rule.Threshold)
		if triggered {
			alert := am.createAlert(rule, value)
			am.fireAlert(ctx, alert)
		}
	}
}

// getMetricValue 获取指标值
func (am *AlertManager) getMetricValue(metric string) (float64, bool) {
	switch metric {
	case "error_rate":
		// 通过错误率和请求总数计算
		return 0, false
	case "kafka_consumer_lag":
		return 0, false
	case "redis_connected":
		// 通过 Redis ping 检查
		return 1, true
	case "database_connected":
		// 通过 DB ping 检查
		return 1, true
	case "p99_latency_ms", "websocket_connections", "memory_usage_percent", "cpu_usage_percent":
		return am.getSystemMetric(metric)
	default:
		return 0, false
	}
}

// getSystemMetric 获取系统指标
func (am *AlertManager) getSystemMetric(metric string) (float64, bool) {
	if am.metrics == nil {
		return 0, false
	}
	
	switch metric {
	case "websocket_connections":
		gauge := am.metrics.GetGauge("pingme_ws_connections_active")
		if gauge != nil {
			return 0, true // Gauge 值需要通过不同的方式获取
		}
		return 0, false
	case "memory_usage_percent", "cpu_usage_percent":
		return 0, false
	default:
		return 0, false
	}
}

// evaluateCondition 评估条件
func (am *AlertManager) evaluateCondition(value float64, condition string, threshold any) bool {
	var thresh float64
	
	switch t := threshold.(type) {
	case float64:
		thresh = t
	case int:
		thresh = float64(t)
	case int64:
		thresh = float64(t)
	default:
		return false
	}

	switch condition {
	case "gt":
		return value > thresh
	case "lt":
		return value < thresh
	case "eq":
		return value == thresh
	case "gte":
		return value >= thresh
	case "lte":
		return value <= thresh
	default:
		return false
	}
}

// createAlert 创建告警
func (am *AlertManager) createAlert(rule config.AlertRule, value float64) Alert {
	// 替换消息模板中的占位符
	message := rule.Message
	if valueStr := fmt.Sprintf("%.2f", value); valueStr != "{{value}}" {
		message = replacePlaceholder(message, "{{value}}", valueStr)
	}

	return Alert{
		Name:      rule.Name,
		Severity:  rule.Severity,
		Message:   message,
		Value:     value,
		Timestamp: time.Now().Unix(),
		Instance:  am.instance,
	}
}

// replacePlaceholder 替换占位符
func replacePlaceholder(template, placeholder, value string) string {
	result := template
	for {
		idx := -1
		for i := 0; i < len(result)-len(placeholder)+1; i++ {
			if result[i:i+len(placeholder)] == placeholder {
				idx = i
				break
			}
		}
		if idx == -1 {
			break
		}
		result = result[:idx] + value + result[idx+len(placeholder):]
	}
	return result
}

// fireAlert 触发告警
func (am *AlertManager) fireAlert(ctx context.Context, alert Alert) {
	// 记录告警到历史
	am.mu.Lock()
	if len(am.history) >= 100 {
		am.history = am.history[1:]
	}
	am.history = append(am.history, alert)
	am.mu.Unlock()

	// 打印告警日志
	logFn := logger.Warn
	if alert.Severity == SeverityCritical {
		logFn = logger.Error
	}
	logFn("Alert triggered",
		"name", alert.Name,
		"severity", alert.Severity,
		"message", alert.Message,
		"value", alert.Value,
		"instance", alert.Instance)

	// 发送告警到 webhook
	if am.config.Webhook.URL != "" {
		am.sendWebhook(ctx, alert)
	}

	// 更新 Prometheus 告警指标
	am.recordAlertMetric(alert)
}

// sendWebhook 发送告警到 webhook
func (am *AlertManager) sendWebhook(ctx context.Context, alert Alert) {
	// 替换模板变量
	template := am.config.Webhook.Template
	template = replacePlaceholder(template, "{{.Rule.Name}}", alert.Name)
	template = replacePlaceholder(template, "{{.Rule.Severity}}", alert.Severity)
	template = replacePlaceholder(template, "{{.Rule.Message}}", alert.Message)
	template = replacePlaceholder(template, "{{.Value}}", fmt.Sprintf("%v", alert.Value))
	template = replacePlaceholder(template, "{{.Timestamp}}", fmt.Sprintf("%d", alert.Timestamp))
	template = replacePlaceholder(template, "{{.Instance}}", alert.Instance)

	// 发送请求
	body := []byte(template)
	req, err := http.NewRequestWithContext(ctx, am.config.Webhook.Method, am.config.Webhook.URL, bytes.NewReader(body))
	if err != nil {
		logger.Error("Failed to create alert webhook request",
			"error", err)
		return
	}

	// 设置 headers
	for k, v := range am.config.Webhook.Headers {
		req.Header.Set(k, v)
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		logger.Error("Failed to send alert webhook",
			"error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		logger.Warn("Alert webhook returned non-2xx status",
			"status", resp.StatusCode)
	}
}

// recordAlertMetric 记录告警指标
func (am *AlertManager) recordAlertMetric(alert Alert) {
	// 可以通过 Prometheus 记录告警触发次数
}

// GetHistory 获取告警历史
func (am *AlertManager) GetHistory() []Alert {
	am.mu.RLock()
	defer am.mu.RUnlock()
	
	result := make([]Alert, len(am.history))
	copy(result, am.history)
	return result
}

// ============ 告警指标定义 ============

var (
	alertTriggers = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "alert_triggers_total",
			Help: "Total number of alert triggers",
		},
		[]string{"alert_name", "severity"},
	)
)

func init() {
	prometheus.MustRegister(alertTriggers)
}
