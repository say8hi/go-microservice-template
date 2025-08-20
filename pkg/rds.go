package storage

import (
	"context"
	"encoding/json"
	"fmt"

	redis "github.com/redis/go-redis/v9"
)

type Input struct {
	ReqID string
	Data  []map[string]interface{}
}

type RedisStorage struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisStorage(addr string, password string, db int) *RedisStorage {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	return &RedisStorage{
		client: rdb,
		ctx:    context.Background(),
	}
}

func (r *RedisStorage) Save(input Input) error {
	dataJSON, err := json.Marshal(input.Data)
	if err != nil {
		return fmt.Errorf("marshal error: %w", err)
	}

	if err := r.client.Set(r.ctx, input.ReqID, dataJSON, 0).Err(); err != nil {
		return fmt.Errorf("redis set error: %w", err)
	}
	return nil
}

func (r *RedisStorage) Get(reqID string) ([]map[string]interface{}, error) {
	val, err := r.client.Get(r.ctx, reqID).Result()
	if err == redis.Nil {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("redis get error: %w", err)
	}

	var data []map[string]interface{}
	if err := json.Unmarshal([]byte(val), &data); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return data, nil
}

func (r *RedisStorage) Delete(reqID string) error {
	if err := r.client.Del(r.ctx, reqID).Err(); err != nil {
		return fmt.Errorf("redis del error: %w", err)
	}
	return nil
}
