package cache

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Cache defines the cache interface
type Cache interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte, ttl time.Duration) error
	// Delete remove a chave (invalidação síncrona após escrita — PRD 0011 RF05).
	Delete(ctx context.Context, key string) error
}

// RedisCache implements Cache using Redis
type RedisCache struct {
	client *redis.Client
	logger *zap.Logger
}

// NewRedisCache creates a new Redis cache
func NewRedisCache(client *redis.Client, logger *zap.Logger) *RedisCache {
	return &RedisCache{
		client: client,
		logger: logger,
	}
}

// Get retrieves a value from Redis cache
func (rc *RedisCache) Get(ctx context.Context, key string) ([]byte, error) {
	// Set a timeout for the cache operation
	cacheCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	val, err := rc.client.Get(cacheCtx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			rc.logger.Info("cache miss",
				zap.String("action", "cache_miss"),
				zap.String("key", key),
			)
			return nil, nil
		}

		// Log timeout or other errors
		rc.logger.Warn("cache get error",
			zap.String("action", "cache_error"),
			zap.String("key", key),
			zap.Error(err),
		)
		return nil, err
	}

	rc.logger.Debug("cache hit",
		zap.String("action", "cache_hit"),
		zap.String("key", key),
	)

	return val, nil
}

// Set stores a value in Redis cache
func (rc *RedisCache) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	// Set a timeout for the cache operation
	cacheCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	err := rc.client.Set(cacheCtx, key, value, ttl).Err()
	if err != nil {
		rc.logger.Warn("cache set error",
			zap.String("action", "cache_set_error"),
			zap.String("key", key),
			zap.Error(err),
		)
		return err
	}

	rc.logger.Debug("cache set success",
		zap.String("action", "cache_set"),
		zap.String("key", key),
		zap.Duration("ttl", ttl),
	)

	return nil
}

// Helper function to marshal data to JSON bytes
func MarshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// Helper function to unmarshal JSON bytes to data
func UnmarshalJSON(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

// Delete remove a chave do Redis (ausência não é erro).
func (rc *RedisCache) Delete(ctx context.Context, key string) error {
	cacheCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	if err := rc.client.Del(cacheCtx, key).Err(); err != nil {
		rc.logger.Warn("cache delete error",
			zap.String("action", "cache_delete_error"),
			zap.String("key", key),
			zap.Error(err),
		)
		return err
	}
	return nil
}
