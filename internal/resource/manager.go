package resource

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"PingMe/internal/logger"
)

// ResourceType 资源类型
type ResourceType string

const (
	ResourceTypeGoroutine  ResourceType = "goroutine"
	ResourceTypeConnection ResourceType = "connection"
	ResourceTypeMemory     ResourceType = "memory"
	ResourceTypeFile       ResourceType = "file"
	ResourceTypeDatabase   ResourceType = "database"
	ResourceTypeRedis      ResourceType = "redis"
	ResourceTypeKafka      ResourceType = "kafka"
)

// ResourceStat 资源统计
type ResourceStat struct {
	Type       ResourceType `json:"type"`
	Name       string       `json:"name"`
	Count      int64        `json:"count"`
	Limit      int64        `json:"limit"`
	Usage      int64        `json:"usage_bytes"`
	LastActive int64        `json:"last_active_unix"`
}

// ResourceCleaner 资源清理器接口
type ResourceCleaner interface {
	Type() ResourceType
	Name() string
	Clean(ctx context.Context) error
	GetStat() ResourceStat
}

// Manager 资源管理器
type Manager struct {
	cleaners map[ResourceType]map[string]ResourceCleaner
	mu        sync.RWMutex
	stats     map[ResourceType]int64
	stopChan  chan struct{}
	wg        sync.WaitGroup
	
	// 配置
	cleanupInterval time.Duration
	enableAutoGC    bool
	gcThreshold    uint64 // MB
}

// NewManager 创建资源管理器
func NewManager(cleanupInterval time.Duration, enableAutoGC bool, gcThreshold uint64) *Manager {
	m := &Manager{
		cleaners:        make(map[ResourceType]map[string]ResourceCleaner),
		stats:           make(map[ResourceType]int64),
		stopChan:        make(chan struct{}),
		cleanupInterval: cleanupInterval,
		enableAutoGC:    enableAutoGC,
		gcThreshold:    gcThreshold,
	}
	
	// 初始化资源类型
	for _, rt := range []ResourceType{
		ResourceTypeGoroutine,
		ResourceTypeConnection,
		ResourceTypeMemory,
		ResourceTypeFile,
		ResourceTypeDatabase,
		ResourceTypeRedis,
		ResourceTypeKafka,
	} {
		m.cleaners[rt] = make(map[string]ResourceCleaner)
		m.stats[rt] = 0
	}
	
	return m
}

// Register 注册资源清理器
func (m *Manager) Register(cleaner ResourceCleaner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	rt := cleaner.Type()
	if _, ok := m.cleaners[rt]; !ok {
		m.cleaners[rt] = make(map[string]ResourceCleaner)
	}
	
	m.cleaners[rt][cleaner.Name()] = cleaner
	logger.Info("Resource cleaner registered",
		"type", rt,
		"name", cleaner.Name())
}

// Unregister 注销资源清理器
func (m *Manager) Unregister(rt ResourceType, name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if cleaners, ok := m.cleaners[rt]; ok {
		delete(cleaners, name)
	}
}

// Start 启动资源管理
func (m *Manager) Start(ctx context.Context) {
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		m.runCleanupLoop(ctx)
	}()
	
	logger.Info("Resource manager started",
		"interval", m.cleanupInterval,
		"auto_gc", m.enableAutoGC)
}

// Stop 停止资源管理
func (m *Manager) Stop() {
	close(m.stopChan)
	m.wg.Wait()
	logger.Info("Resource manager stopped")
}

// runCleanupLoop 运行清理循环
func (m *Manager) runCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(m.cleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.cleanup(ctx)
			
			// 自动 GC
			if m.enableAutoGC {
				m.checkAndRunGC()
			}
		}
	}
}

// cleanup 执行清理
func (m *Manager) cleanup(ctx context.Context) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	totalCleaned := 0
	failedCleaners := 0
	
	for rt, cleaners := range m.cleaners {
		for name, cleaner := range cleaners {
			if err := cleaner.Clean(ctx); err != nil {
				logger.Warn("Resource cleanup failed",
					"type", rt,
					"name", name,
					"error", err)
				failedCleaners++
			} else {
				totalCleaned++
			}
		}
		
		// 更新统计
		stat := m.stats[rt]
		m.stats[rt] = stat + 1
	}
	
	if totalCleaned > 0 || failedCleaners > 0 {
		logger.Debug("Resource cleanup completed",
			"cleaned", totalCleaned,
			"failed", failedCleaners)
	}
}

// checkAndRunGC 检查并运行 GC
func (m *Manager) checkAndRunGC() {
	var mstats runtime.MemStats
	runtime.ReadMemStats(&mstats)
	
	// 转换为 MB
	allocMB := int64(mstats.Alloc / (1024 * 1024))
	thresholdMB := int64(m.gcThreshold)
	
	if allocMB > thresholdMB {
		logger.Info("Memory threshold exceeded, running GC",
			"alloc_mb", allocMB,
			"threshold_mb", m.gcThreshold)
		
		// 记录 GC 前状态
		beforeGC := mstats.NumGC
		
		// 强制 GC
		runtime.GC()
		
		// 等待 GC 完成
		runtime.GC()
		
		// 记录 GC 后状态
		runtime.ReadMemStats(&mstats)
		afterGC := mstats.NumGC
		
		logger.Info("GC completed",
			"before_gc", beforeGC,
			"after_gc", afterGC,
			"alloc_mb", mstats.Alloc/(1024*1024))
	}
}

// GetStats 获取资源统计
func (m *Manager) GetStats() map[ResourceType]ResourceStat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	result := make(map[ResourceType]ResourceStat)
	for rt, cleaners := range m.cleaners {
		count := int64(len(cleaners))
		var lastActive int64
		
		for _, cleaner := range cleaners {
			stat := cleaner.GetStat()
			if stat.LastActive > lastActive {
				lastActive = stat.LastActive
			}
		}
		
		result[rt] = ResourceStat{
			Type:       rt,
			Count:      count,
			LastActive: lastActive,
		}
	}
	
	// 添加运行时统计
	result[ResourceTypeGoroutine] = ResourceStat{
		Type:       ResourceTypeGoroutine,
		Name:       "goroutine",
		Count:      int64(runtime.NumGoroutine()),
		LastActive: time.Now().Unix(),
	}
	
	return result
}

// GetMemoryStats 获取内存统计
func (m *Manager) GetMemoryStats() map[string]uint64 {
	var mstats runtime.MemStats
	runtime.ReadMemStats(&mstats)
	
	return map[string]uint64{
		"alloc":        mstats.Alloc,
		"total_alloc":  mstats.TotalAlloc,
		"sys":          mstats.Sys,
		"num_gc":       uint64(mstats.NumGC),
		"num_goroutine": uint64(runtime.NumGoroutine()),
	}
}

// ============ 常用资源清理器 ============

// ConnectionCleaner 连接清理器
type ConnectionCleaner struct {
	name     string
	cleanFn  func() (int, error)
	lastCnt  atomic.Int64
	lastTime atomic.Int64
}

func NewConnectionCleaner(name string, cleanFn func() (int, error)) *ConnectionCleaner {
	return &ConnectionCleaner{
		name:    name,
		cleanFn: cleanFn,
	}
}

func (c *ConnectionCleaner) Type() ResourceType   { return ResourceTypeConnection }
func (c *ConnectionCleaner) Name() string         { return c.name }

func (c *ConnectionCleaner) Clean(ctx context.Context) error {
	if c.cleanFn == nil {
		return nil
	}
	
	cnt, err := c.cleanFn()
	c.lastCnt.Store(int64(cnt))
	c.lastTime.Store(time.Now().UnixMilli())
	
	return err
}

func (c *ConnectionCleaner) GetStat() ResourceStat {
	return ResourceStat{
		Type:       ResourceTypeConnection,
		Name:       c.name,
		Count:      c.lastCnt.Load(),
		LastActive: c.lastTime.Load(),
	}
}

// BufferPoolCleaner 缓冲区池清理器
type BufferPoolCleaner struct {
	name string
	size atomic.Int64
}

func NewBufferPoolCleaner(name string) *BufferPoolCleaner {
	return &BufferPoolCleaner{name: name}
}

func (b *BufferPoolCleaner) Type() ResourceType { return ResourceTypeMemory }
func (b *BufferPoolCleaner) Name() string       { return b.name }

func (b *BufferPoolCleaner) Clean(ctx context.Context) error {
	// 缓冲区池自动管理，不需要手动清理
	b.size.Store(0)
	return nil
}

func (b *BufferPoolCleaner) GetStat() ResourceStat {
	return ResourceStat{
		Type:  ResourceTypeMemory,
		Name:  b.name,
		Count: b.size.Load(),
	}
}

func (b *BufferPoolCleaner) IncrSize(n int64) {
	b.size.Add(n)
}

func (b *BufferPoolCleaner) DecrSize(n int64) {
	b.size.Add(-n)
}
