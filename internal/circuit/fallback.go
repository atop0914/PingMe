package circuit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"PingMe/internal/logger"
)

// FallbackType 降级类型
type FallbackType string

const (
	FallbackTypeNone     FallbackType = "none"      // 无降级
	FallbackTypeCache    FallbackType = "cache"     // 缓存降级
	FallbackTypeDefault  FallbackType = "default"    // 默认值降级
	FallbackTypeDeferred FallbackType = "deferred"  // 延迟处理降级
	FallbackTypeCircuit FallbackType = "circuit"    // 熔断降级
)

// FallbackHandler 降级处理器
type FallbackHandler func(ctx context.Context, err error) (any, error)

// FallbackConfig 降级配置
type FallbackConfig struct {
	Enabled       bool          `yaml:"enabled"`
	Type          FallbackType  `yaml:"type"`
	CacheTTL      time.Duration `yaml:"cache_ttl"`      // 缓存降级的 TTL
	RetryDelay    time.Duration `yaml:"retry_delay"`   // 延迟重试延迟
	MaxRetries    int           `yaml:"max_retries"`   // 最大重试次数
	DefaultValue  any            `yaml:"default_value"` // 默认值
}

// DefaultFallbackConfig 返回默认降级配置
func DefaultFallbackConfig() *FallbackConfig {
	return &FallbackConfig{
		Enabled:    true,
		Type:       FallbackTypeDefault,
		CacheTTL:   5 * time.Minute,
		RetryDelay: 1 * time.Second,
		MaxRetries: 3,
	}
}

// FallbackManager 降级管理器
type FallbackManager struct {
	config  *FallbackConfig
	cache   map[string]*cacheEntry
	mu      sync.RWMutex
	onDefer func(ctx context.Context, req any) // 延迟处理回调
}

type cacheEntry struct {
	value      any
	expiration time.Time
}

// NewFallbackManager 创建降级管理器
func NewFallbackManager(cfg *FallbackConfig) *FallbackManager {
	if cfg == nil {
		cfg = DefaultFallbackConfig()
	}
	
	m := &FallbackManager{
		config: cfg,
		cache:  make(map[string]*cacheEntry),
	}
	
	// 启动缓存清理
	if cfg.Type == FallbackTypeCache {
		go m.cleanupLoop()
	}
	
	return m
}

// Handle 处理降级
func (m *FallbackManager) Handle(ctx context.Context, key string, err error, fallback FallbackHandler) (any, error) {
	if !m.config.Enabled {
		return nil, err
	}
	
	switch m.config.Type {
	case FallbackTypeCache:
		return m.handleCache(ctx, key, err)
	case FallbackTypeDefault:
		return m.handleDefault(ctx, err)
	case FallbackTypeDeferred:
		return m.handleDeferred(ctx, key, err)
	case FallbackTypeCircuit:
		return m.handleCircuit(ctx, key, err)
	default:
		return nil, err
	}
}

// handleCache 缓存降级
func (m *FallbackManager) handleCache(ctx context.Context, key string, err error) (any, error) {
	// 尝试从缓存获取
	m.mu.RLock()
	entry, ok := m.cache[key]
	m.mu.RUnlock()
	
	if ok && time.Now().Before(entry.expiration) {
		logger.Info("Fallback: using cached value",
			"key", key)
		return entry.value, nil
	}
	
	// 无缓存，返回错误
	logger.Warn("Fallback: cache miss",
		"key", key,
		"error", err)
	
	// 触发延迟处理
	if m.onDefer != nil {
		m.onDefer(ctx, key)
	}
	
	return nil, err
}

// handleDefault 默认值降级
func (m *FallbackManager) handleDefault(ctx context.Context, err error) (any, error) {
	logger.Warn("Fallback: using default value",
		"error", err)
	
	if m.config.DefaultValue != nil {
		return m.config.DefaultValue, nil
	}
	
	return nil, err
}

// handleDeferred 延迟处理降级
func (m *FallbackManager) handleDeferred(ctx context.Context, key string, err error) (any, error) {
	logger.Warn("Fallback: deferring operation",
		"key", key,
		"error", err)
	
	// 触发延迟处理回调
	if m.onDefer != nil {
		m.onDefer(ctx, key)
	}
	
	// 返回临时结果
	return map[string]interface{}{
		"deferred": true,
		"key":      key,
		"retry_at": time.Now().Add(m.config.RetryDelay).Unix(),
	}, nil
}

// handleCircuit 熔断降级
func (m *FallbackManager) handleCircuit(ctx context.Context, key string, err error) (any, error) {
	logger.Warn("Fallback: circuit breaker triggered",
		"key", key,
		"error", err)
	
	// 熔断降级通常返回友好错误
	return nil, fmt.Errorf("service temporarily unavailable: %v", err)
}

// SetCache 设置缓存
func (m *FallbackManager) SetCache(key string, value any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.cache[key] = &cacheEntry{
		value:      value,
		expiration: time.Now().Add(m.config.CacheTTL),
	}
}

// GetCache 获取缓存
func (m *FallbackManager) GetCache(key string) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	entry, ok := m.cache[key]
	if !ok || time.Now().After(entry.expiration) {
		return nil, false
	}
	
	return entry.value, true
}

// DeleteCache 删除缓存
func (m *FallbackManager) DeleteCache(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	delete(m.cache, key)
}

// SetDeferCallback 设置延迟处理回调
func (m *FallbackManager) SetDeferCallback(fn func(ctx context.Context, req any)) {
	m.onDefer = fn
}

// cleanupLoop 缓存清理循环
func (m *FallbackManager) cleanupLoop() {
	ticker := time.NewTicker(m.config.CacheTTL / 2)
	defer ticker.Stop()
	
	for range ticker.C {
		m.mu.Lock()
		now := time.Now()
		for key, entry := range m.cache {
			if now.After(entry.expiration) {
				delete(m.cache, key)
			}
		}
		m.mu.Unlock()
	}
}

// GetStats 获取降级统计
func (m *FallbackManager) GetStats() map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	return map[string]any{
		"enabled":   m.config.Enabled,
		"type":      m.config.Type,
		"cache_size": len(m.cache),
	}
}
