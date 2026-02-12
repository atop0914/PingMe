package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"PingMe/internal/config"
	"PingMe/internal/logger"

	"github.com/redis/go-redis/v9"
)

// UserPresence 用户在线状态
type UserPresence struct {
	UserID     string `json:"user_id"`
	InstanceID string `json:"instance_id"` // 实例ID，用于多实例路由
	ConnID     string `json:"conn_id"`     // 连接ID
	TTL        int    `json:"ttl"`         // 过期时间（秒）
	UpdatedAt  int64  `json:"updated_at"`  // 更新时间戳
}

// Client Redis 客户端封装
type Client struct {
	client    *redis.Client
	cfg       *config.Config
	mu        sync.RWMutex
	localCache map[string]*cacheEntry // 本地缓存
	instanceID string                  // 当前实例ID
}

type cacheEntry struct {
	presence *UserPresence
	expiry   time.Time
}

// NewClient 创建 Redis 客户端
func NewClient(cfg *config.Config, instanceID string) (*Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Addr(),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})

	// 测试连接
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Info("Redis client connected",
		"addr", cfg.Redis.Addr(),
		"instance_id", instanceID)

	return &Client{
		client:     client,
		cfg:        cfg,
		localCache: make(map[string]*cacheEntry),
		instanceID: instanceID,
	}, nil
}

// Close 关闭 Redis 连接
func (c *Client) Close() error {
	return c.client.Close()
}

// SetUserOnline 设置用户在线状态
func (c *Client) SetUserOnline(ctx context.Context, userID, connID string, ttlSeconds int) error {
	presence := &UserPresence{
		UserID:     userID,
		InstanceID: c.instanceID,
		ConnID:     connID,
		TTL:        ttlSeconds,
		UpdatedAt:  time.Now().UnixMilli(),
	}

	data, err := json.Marshal(presence)
	if err != nil {
		return fmt.Errorf("failed to marshal presence: %w", err)
	}

	key := c.userPresenceKey(userID)
	if err := c.client.Set(ctx, key, data, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to set user online: %w", err)
	}

	logger.Debug("User online status set",
		"user_id", userID,
		"conn_id", connID,
		"instance_id", c.instanceID)

	return nil
}

// SetUserOffline 设置用户离线状态
func (c *Client) SetUserOffline(ctx context.Context, userID string) error {
	key := c.userPresenceKey(userID)
	if err := c.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("failed to set user offline: %w", err)
	}

	logger.Debug("User offline status set",
		"user_id", userID)

	// 清除本地缓存
	c.mu.Lock()
	delete(c.localCache, userID)
	c.mu.Unlock()

	return nil
}

// GetUserPresence 获取用户在线状态（带本地缓存）
func (c *Client) GetUserPresence(ctx context.Context, userID string) (*UserPresence, error) {
	// 先检查本地缓存
	c.mu.RLock()
	entry, ok := c.localCache[userID]
	if ok && time.Now().Before(entry.expiry) {
		c.mu.RUnlock()
		return entry.presence, nil
	}
	c.mu.RUnlock()

	// 本地缓存未命中，从 Redis 获取
	key := c.userPresenceKey(userID)
	data, err := c.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // 用户离线
		}
		return nil, fmt.Errorf("failed to get user presence: %w", err)
	}

	var presence UserPresence
	if err := json.Unmarshal(data, &presence); err != nil {
		return nil, fmt.Errorf("failed to unmarshal presence: %w", err)
	}

	// 更新本地缓存
	c.mu.Lock()
	c.localCache[userID] = &cacheEntry{
		presence: &presence,
		expiry:   time.Now().Add(5 * time.Second), // 缓存 5 秒
	}
	c.mu.Unlock()

	return &presence, nil
}

// RefreshUserTTL 刷新用户在线状态 TTL
func (c *Client) RefreshUserTTL(ctx context.Context, userID string, ttlSeconds int) error {
	key := c.userPresenceKey(userID)
	if err := c.client.Expire(ctx, key, time.Duration(ttlSeconds)*time.Second).Err(); err != nil {
		return fmt.Errorf("failed to refresh user TTL: %w", err)
	}

	// 更新本地缓存过期时间
	c.mu.Lock()
	if entry, ok := c.localCache[userID]; ok {
		entry.expiry = time.Now().Add(time.Duration(ttlSeconds) * time.Second)
	}
	c.mu.Unlock()

	return nil
}

// IsUserOnline 判断用户是否在线（使用本地缓存）
func (c *Client) IsUserOnline(ctx context.Context, userID string) (bool, error) {
	presence, err := c.GetUserPresence(ctx, userID)
	if err != nil {
		return false, err
	}
	return presence != nil, nil
}

// GetUsersOnline 批量获取用户在线状态
func (c *Client) GetUsersOnline(ctx context.Context, userIDs []string) (map[string]*UserPresence, error) {
	if len(userIDs) == 0 {
		return make(map[string]*UserPresence), nil
	}

	// 先从本地缓存获取
	result := make(map[string]*UserPresence)
	missedIDs := make([]string, 0)

	c.mu.RLock()
	for _, userID := range userIDs {
		entry, ok := c.localCache[userID]
		if ok && time.Now().Before(entry.expiry) {
			result[userID] = entry.presence
		} else {
			missedIDs = append(missedIDs, userID)
		}
	}
	c.mu.RUnlock()

	if len(missedIDs) == 0 {
		return result, nil
	}

	// 批量从 Redis 获取
	pipe := c.client.Pipeline()
	cmds := make(map[string]*redis.StringCmd)

	for _, userID := range missedIDs {
		key := c.userPresenceKey(userID)
		cmds[userID] = pipe.Get(ctx, key)
	}

	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return nil, fmt.Errorf("failed to batch get user presence: %w", err)
	}

	for userID, cmd := range cmds {
		data, err := cmd.Bytes()
		if err != nil {
			if err == redis.Nil {
				continue // 用户离线
			}
			logger.Warn("Failed to get user presence",
				"user_id", userID,
				"error", err)
			continue
		}

		var presence UserPresence
		if err := json.Unmarshal(data, &presence); err != nil {
			logger.Warn("Failed to unmarshal presence",
				"user_id", userID,
				"error", err)
			continue
		}

		result[userID] = &presence

		// 更新本地缓存
		c.mu.Lock()
		c.localCache[userID] = &cacheEntry{
			presence: &presence,
			expiry:   time.Now().Add(5 * time.Second),
		}
		c.mu.Unlock()
	}

	return result, nil
}

// GetOnlineUsers 获取所有在线用户（扫描 Redis）
func (c *Client) GetOnlineUsers(ctx context.Context) ([]*UserPresence, error) {
	var cursor uint64
	var allPresences []*UserPresence

	pattern := c.userPresenceKey("*")
	for {
		keys, nextCursor, err := c.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan online users: %w", err)
		}

		if len(keys) > 0 {
			// 批量获取
			values, err := c.client.MGet(ctx, keys...).Result()
			if err != nil {
				return nil, fmt.Errorf("failed to mget presence: %w", err)
			}

			for _, v := range values {
				if v == nil {
					continue
				}

				data, ok := v.(string)
				if !ok {
					continue
				}

				var presence UserPresence
				if err := json.Unmarshal([]byte(data), &presence); err != nil {
					continue
				}

				allPresences = append(allPresences, &presence)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return allPresences, nil
}

// GetLocalOnlineUsers 获取本实例在线用户
func (c *Client) GetLocalOnlineUsers(ctx context.Context) ([]string, error) {
	presences, err := c.GetOnlineUsers(ctx)
	if err != nil {
		return nil, err
	}

	var userIDs []string
	for _, p := range presences {
		if p.InstanceID == c.instanceID {
			userIDs = append(userIDs, p.UserID)
		}
	}

	return userIDs, nil
}

// GetStats 获取 Redis 统计信息
func (c *Client) GetStats() map[string]interface{} {
	ctx := context.Background()
	return map[string]interface{}{
		"redis_version":       c.client.Info(ctx, "server").Val(),
		"connected_clients":   c.client.Info(ctx, "clients").Val(),
	}
}

// userPresenceKey 生成用户在线状态 key
func (c *Client) userPresenceKey(userID string) string {
	return fmt.Sprintf("pingme:presence:%s", userID)
}

// CleanupLocalCache 清理过期本地缓存
func (c *Client) CleanupLocalCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for userID, entry := range c.localCache {
		if now.After(entry.expiry) {
			delete(c.localCache, userID)
		}
	}
}
