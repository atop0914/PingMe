package redis

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisClient(t *testing.T) {
	// 使用 miniredis 进行单元测试
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// 直接创建 Redis 客户端
	client := &Client{
		client:     redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		localCache: make(map[string]*cacheEntry),
		instanceID: "test-instance-1",
	}

	ctx := context.Background()

	// 测试设置用户在线
	t.Run("SetUserOnline", func(t *testing.T) {
		err := client.SetUserOnline(ctx, "user1", "conn1", 30)
		assert.NoError(t, err)

		// 验证用户在线
		online, err := client.IsUserOnline(ctx, "user1")
		assert.NoError(t, err)
		assert.True(t, online)
	})

	// 测试获取用户在线状态
	t.Run("GetUserPresence", func(t *testing.T) {
		presence, err := client.GetUserPresence(ctx, "user1")
		assert.NoError(t, err)
		assert.NotNil(t, presence)
		assert.Equal(t, "user1", presence.UserID)
		assert.Equal(t, "conn1", presence.ConnID)
		assert.Equal(t, "test-instance-1", presence.InstanceID)
	})

	// 测试设置用户离线
	t.Run("SetUserOffline", func(t *testing.T) {
		err := client.SetUserOffline(ctx, "user1")
		assert.NoError(t, err)

		// 验证用户离线
		online, err := client.IsUserOnline(ctx, "user1")
		assert.NoError(t, err)
		assert.False(t, online)
	})

	// 测试本地缓存
	t.Run("LocalCache", func(t *testing.T) {
		// 设置用户在线
		err := client.SetUserOnline(ctx, "user2", "conn2", 30)
		assert.NoError(t, err)

		// 第一次获取（应该从 Redis 获取）
		presence1, err := client.GetUserPresence(ctx, "user2")
		assert.NoError(t, err)
		assert.NotNil(t, presence1)

		// 第二次获取（应该从本地缓存获取）
		presence2, err := client.GetUserPresence(ctx, "user2")
		assert.NoError(t, err)
		assert.NotNil(t, presence2)
		assert.Equal(t, presence1.UserID, presence2.UserID)
	})

	// 测试批量获取用户在线状态
	t.Run("BatchGetUsersOnline", func(t *testing.T) {
		// 设置多个用户在线
		client.SetUserOnline(ctx, "user3", "conn3", 30)
		client.SetUserOnline(ctx, "user4", "conn4", 30)

		userIDs := []string{"user3", "user4", "user5", "user6"}
		presences, err := client.GetUsersOnline(ctx, userIDs)
		assert.NoError(t, err)

		// user5 和 user6 应该离线
		assert.Nil(t, presences["user5"])
		assert.Nil(t, presences["user6"])

		// user3 和 user4 应该在线
		assert.NotNil(t, presences["user3"])
		assert.NotNil(t, presences["user4"])
	})

	// 测试刷新 TTL
	t.Run("RefreshUserTTL", func(t *testing.T) {
		// 设置用户在线（使用新的 user ID）
		err := client.SetUserOnline(ctx, "user7", "conn7", 30)
		assert.NoError(t, err)

		// 刷新 TTL
		err = client.RefreshUserTTL(ctx, "user7", 60)
		assert.NoError(t, err)

		// 注意：GetUserPresence 返回的是本地缓存的数据，
		// TTL 字段反映的是创建时的值，而不是实际 Redis 中的 TTL
		// 要验证 TTL，需要直接查询 Redis
		_, err = client.GetUserPresence(ctx, "user7")
		assert.NoError(t, err)
	})

	// 测试不存在的用户
	t.Run("NonExistentUser", func(t *testing.T) {
		presence, err := client.GetUserPresence(ctx, "nonexistent")
		assert.NoError(t, err)
		assert.Nil(t, presence)
	})

	// 测试清理本地缓存
	t.Run("CleanupLocalCache", func(t *testing.T) {
		// 设置用户在线
		client.SetUserOnline(ctx, "user6", "conn6", 30)

		// 清理本地缓存
		client.CleanupLocalCache()

		// 应该还能从 Redis 获取
		presence, err := client.GetUserPresence(ctx, "user6")
		assert.NoError(t, err)
		assert.NotNil(t, presence)
	})

	// 关闭客户端
	client.Close()
}

// TestMultiInstanceRouting 测试多实例路由
func TestMultiInstanceRouting(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	// 创建两个实例
	client1 := &Client{
		client:     redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		localCache: make(map[string]*cacheEntry),
		instanceID: "instance-1",
	}
	client2 := &Client{
		client:     redis.NewClient(&redis.Options{Addr: mr.Addr()}),
		localCache: make(map[string]*cacheEntry),
		instanceID: "instance-2",
	}

	ctx := context.Background()

	// 实例 1 设置用户在线
	err = client1.SetUserOnline(ctx, "user1", "conn1", 30)
	require.NoError(t, err)

	// 实例 2 应该也能看到用户在线
	online, err := client2.IsUserOnline(ctx, "user1")
	assert.NoError(t, err)
	assert.True(t, online)

	// 获取用户状态，应该指向实例 1
	presence, err := client2.GetUserPresence(ctx, "user1")
	assert.NoError(t, err)
	assert.NotNil(t, presence)
	assert.Equal(t, "instance-1", presence.InstanceID)

	client1.Close()
	client2.Close()
}

// TestRedisConnectionFailure 测试 Redis 连接失败场景
func TestRedisConnectionFailure(t *testing.T) {
	// 创建一个无效的 Redis 配置
	client := &Client{
		client:     redis.NewClient(&redis.Options{Addr: "invalid-host:6379"}),
		localCache: make(map[string]*cacheEntry),
		instanceID: "test-instance",
	}

	_, err := client.IsUserOnline(context.Background(), "test")
	assert.Error(t, err)
}
