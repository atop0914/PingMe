package circuit

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"PingMe/internal/logger"
)

var (
	// ErrCircuitOpen 熔断器开启错误
	ErrCircuitOpen = errors.New("circuit breaker is open")
	// ErrTimeout 超时错误
	ErrTimeout = errors.New("operation timeout")
	// ErrFallback 降级错误
	ErrFallback = errors.New("fallback error")
)

// State 熔断器状态
type State int32

const (
	StateClosed State = iota // 关闭状态，正常
	StateHalfOpen // 半开状态，尝试恢复
	StateOpen     // 开启状态，拒绝请求
)

// String 返回状态字符串
func (s State) String() string {
	switch s {
	case StateClosed:
		return "closed"
	case StateHalfOpen:
		return "half_open"
	case StateOpen:
		return "open"
	default:
		return "unknown"
	}
}

// Config 熔断器配置
type Config struct {
	FailureThreshold  int           // 失败次数阈值，达到后打开熔断器
	SuccessThreshold int           // 成功次数阈值，达到后关闭熔断器
	Timeout          time.Duration // 熔断器打开后的超时时间
	HalfOpenMaxCalls int           // 半开状态下的最大并发调用数
}

// DefaultConfig 返回默认配置
func DefaultConfig() *Config {
	return &Config{
		FailureThreshold:  5,   // 连续 5 次失败打开熔断
		SuccessThreshold:  3,   // 连续 3 次成功关闭熔断
		Timeout:           30 * time.Second, // 30 秒后尝试半开
		HalfOpenMaxCalls:  3,   // 半开状态下最多 3 个并发调用
	}
}

// Breaker 熔断器
type Breaker struct {
	name             string
	config           *Config
	state            atomic.Int32
	failureCount     atomic.Int32
	successCount     atomic.Int32
	lastFailureTime  atomic.Int64
	lastSuccessTime  atomic.Int64
	halfOpenCalls    atomic.Int32
	lastStateChange  atomic.Int64
	mu               sync.RWMutex
	onStateChange    func(from, to State)
	onFailure        func(err error)
	onSuccess        func()
}

// NewBreaker 创建熔断器
func NewBreaker(name string, cfg *Config) *Breaker {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	return &Breaker{
		name:   name,
		config: cfg,
	}
}

// State 获取当前状态
func (b *Breaker) State() State {
	return State(b.state.Load())
}

// IsOpen 检查熔断器是否开启
func (b *Breaker) IsOpen() bool {
	return b.State() == StateOpen
}

// Execute 执行操作，如果熔断器开启则返回错误
func (b *Breaker) Execute(ctx context.Context, fn func() error) error {
	// 检查超时
	if b.IsOpen() {
		// 检查是否应该进入半开状态
		if b.shouldAttemptReset() {
			b.tryOpenHalf()
		}
		
		// 返回熔断器开启错误
		logger.Warn("Circuit breaker open, rejecting request",
			"breaker", b.name,
			"state", b.State().String())
		
		return ErrCircuitOpen
	}
	
	// 尝试获取半开状态的许可
	if !b.acquireHalfOpenPermit(ctx) {
		return ErrCircuitOpen
	}
	
	// 执行操作
	err := fn()
	
	// 记录结果
	if err != nil {
		b.recordFailure()
	} else {
		b.recordSuccess()
	}
	
	return err
}

// ExecuteWithFallback 执行操作，支持降级
func (b *Breaker) ExecuteWithFallback(ctx context.Context, fn func() error, fallback func(err error) error) error {
	err := b.Execute(ctx, fn)
	
	if err != nil {
		// 如果有降级函数，调用降级
		if fallback != nil {
			logger.Info("Executing fallback",
				"breaker", b.name,
				"error", err)
			return fallback(err)
		}
	}
	
	return err
}

// shouldAttemptReset 检查是否应该尝试重置
func (b *Breaker) shouldAttemptReset() bool {
	lastChange := b.lastStateChange.Load()
	if lastChange == 0 {
		return true
	}
	
	elapsed := time.Since(time.Unix(0, lastChange))
	return elapsed >= b.config.Timeout
}

// tryOpenHalf 尝试进入半开状态
func (b *Breaker) tryOpenHalf() {
	b.state.CompareAndSwap(int32(StateOpen), int32(StateHalfOpen))
	logger.Info("Circuit breaker attempting reset",
		"breaker", b.name)
}

// acquireHalfOpenPermit 获取半开状态许可
func (b *Breaker) acquireHalfOpenPermit(ctx context.Context) bool {
	// 如果不是半开状态，直接返回 true
	if b.State() != StateHalfOpen {
		return true
	}
	
	// 尝试获取许可
	for {
		current := b.halfOpenCalls.Load()
		if current >= int32(b.config.HalfOpenMaxCalls) {
			return false
		}
		
		if b.halfOpenCalls.CompareAndSwap(current, current+1) {
			defer b.halfOpenCalls.Add(-1)
			return true
		}
	}
}

// recordFailure 记录失败
func (b *Breaker) recordFailure() {
	b.failureCount.Add(1)
	b.successCount.Store(0)
	b.lastFailureTime.Store(time.Now().UnixMilli())
	
	// 连续失败次数达到阈值，打开熔断器
	if b.failureCount.Load() >= int32(b.config.FailureThreshold) {
		b.open()
	}
	
	if b.onFailure != nil {
		b.onFailure(ErrCircuitOpen)
	}
}

// recordSuccess 记录成功
func (b *Breaker) recordSuccess() {
	b.successCount.Add(1)
	b.failureCount.Store(0)
	b.lastSuccessTime.Store(time.Now().UnixMilli())
	
	// 连续成功次数达到阈值，关闭熔断器
	if b.State() == StateHalfOpen && b.successCount.Load() >= int32(b.config.SuccessThreshold) {
		b.close()
	}
}

// open 打开熔断器
func (b *Breaker) open() {
	oldState := b.State()
	if oldState == StateOpen {
		return
	}
	
	b.state.Store(int32(StateOpen))
	b.failureCount.Store(0)
	b.lastStateChange.Store(time.Now().UnixMilli())
	
	logger.Warn("Circuit breaker opened",
		"breaker", b.name,
		"failure_count", b.failureCount.Load())
	
	if b.onStateChange != nil {
		b.onStateChange(oldState, StateOpen)
	}
}

// close 关闭熔断器
func (b *Breaker) close() {
	oldState := b.State()
	if oldState == StateClosed {
		return
	}
	
	b.state.Store(int32(StateClosed))
	b.successCount.Store(0)
	b.lastStateChange.Store(time.Now().UnixMilli())
	
	logger.Info("Circuit breaker closed",
		"breaker", b.name)
	
	if b.onStateChange != nil {
		b.onStateChange(oldState, StateClosed)
	}
}

// Reset 重置熔断器
func (b *Breaker) Reset() {
	b.state.Store(int32(StateClosed))
	b.failureCount.Store(0)
	b.successCount.Store(0)
	b.lastStateChange.Store(0)
}

// GetStats 获取熔断器状态
func (b *Breaker) GetStats() map[string]any {
	return map[string]any{
		"name":              b.name,
		"state":             b.State().String(),
		"failure_count":     b.failureCount.Load(),
		"success_count":     b.successCount.Load(),
		"last_failure_time": b.lastFailureTime.Load(),
		"last_success_time": b.lastSuccessTime.Load(),
	}
}

// SetStateChangeCallback 设置状态变更回调
func (b *Breaker) SetStateChangeCallback(fn func(from, to State)) {
	b.onStateChange = fn
}

// SetFailureCallback 设置失败回调
func (b *Breaker) SetFailureCallback(fn func(err error)) {
	b.onFailure = fn
}

// SetSuccessCallback 设置成功回调
func (b *Breaker) SetSuccessCallback(fn func()) {
	b.onSuccess = fn
}

// ============ 熔断器管理器 ============

// Manager 熔断器管理器
type Manager struct {
	breakers map[string]*Breaker
	mu       sync.RWMutex
	config   *Config
}

// NewManager 创建熔断器管理器
func NewManager(cfg *Config) *Manager {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	
	return &Manager{
		breakers: make(map[string]*Breaker),
		config:   cfg,
	}
}

// Get 获取或创建熔断器
func (m *Manager) Get(name string) *Breaker {
	m.mu.RLock()
	b, ok := m.breakers[name]
	m.mu.RUnlock()
	
	if ok {
		return b
	}
	
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// 双重检查
	if b, ok := m.breakers[name]; ok {
		return b
	}
	
	b = NewBreaker(name, m.config)
	m.breakers[name] = b
	
	logger.Info("Created new circuit breaker",
		"name", name)
	
	return b
}

// Remove 移除熔断器
func (m *Manager) Remove(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.breakers, name)
}

// GetAll 获取所有熔断器状态
func (m *Manager) GetAll() map[string]map[string]any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make(map[string]map[string]any)
	for name, breaker := range m.breakers {
		result[name] = breaker.GetStats()
	}
	
	return result
}

// ============ 超时工具 ============

// WithTimeout 带超时的执行
func WithTimeout(ctx context.Context, timeout time.Duration, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	
	done := make(chan error, 1)
	
	go func() {
		done <- fn()
	}()
	
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrTimeout, ctx.Err())
	case err := <-done:
		return err
	}
}

// WithDeadline 带截止时间的执行
func WithDeadline(ctx context.Context, deadline time.Time, fn func() error) error {
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	
	done := make(chan error, 1)
	
	go func() {
		done <- fn()
	}()
	
	select {
	case <-ctx.Done():
		return fmt.Errorf("%w: %v", ErrTimeout, ctx.Err())
	case err := <-done:
		return err
	}
}
