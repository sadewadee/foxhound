package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"

	foxhound "github.com/foxhound-scraper/foxhound"
)

// RedisQueue implements foxhound.Queue using a Redis sorted set.
//
// Jobs are scored so that higher priority always surfaces first. Within a
// priority tier, FIFO order is preserved by incorporating a microsecond
// timestamp into the score:
//
//	score = -(priority * 1_000_000_000 + created_at_micros)
//
// The negation makes ZPOPMIN return the job with the highest effective
// priority.  Distributed workers can safely share the same key because
// ZPOPMIN is atomic.
type RedisQueue struct {
	client *redis.Client
	key    string
}

// NewRedis creates a RedisQueue that connects to the given Redis address.
// The connection is tested with a PING; an error is returned if Redis is
// unavailable.
func NewRedis(addr, password string, db int, queueKey string) (*RedisQueue, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("queue: redis ping: %w", err)
	}
	return NewRedisFromClient(client, queueKey), nil
}

// NewRedisFromClient creates a RedisQueue from an existing redis.Client.
// Ownership of the client is transferred to the queue; Close will close it.
func NewRedisFromClient(client *redis.Client, queueKey string) *RedisQueue {
	return &RedisQueue{client: client, key: queueKey}
}

// Push serialises the job as JSON and adds it to the sorted set with a score
// that encodes priority and FIFO order.
func (q *RedisQueue) Push(ctx context.Context, job *foxhound.Job) error {
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now()
	}

	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("queue: redis push: marshal job: %w", err)
	}

	// Score: negate so lower score = higher priority in ZPOPMIN.
	// Multiply priority by a large factor and add microseconds for FIFO tiebreak.
	micros := job.CreatedAt.UnixMicro()
	score := -(float64(job.Priority)*1_000_000_000 - float64(micros))

	if err := q.client.ZAdd(ctx, q.key, redis.Z{
		Score:  score,
		Member: string(data),
	}).Err(); err != nil {
		return fmt.Errorf("queue: redis push: zadd: %w", err)
	}

	slog.Debug("queue: redis pushed job", "id", job.ID, "priority", job.Priority)
	return nil
}

// Pop atomically removes and returns the highest-priority job.
// It polls every 100 ms when the queue is empty, respecting context cancellation.
func (q *RedisQueue) Pop(ctx context.Context) (*foxhound.Job, error) {
	for {
		// Check context before polling.
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		results, err := q.client.ZPopMin(ctx, q.key, 1).Result()
		if err != nil {
			return nil, fmt.Errorf("queue: redis pop: zpopmin: %w", err)
		}

		if len(results) == 1 {
			raw, ok := results[0].Member.(string)
			if !ok {
				return nil, fmt.Errorf("queue: redis pop: unexpected member type %T", results[0].Member)
			}
			var job foxhound.Job
			if err := json.Unmarshal([]byte(raw), &job); err != nil {
				return nil, fmt.Errorf("queue: redis pop: unmarshal job: %w", err)
			}
			slog.Debug("queue: redis popped job", "id", job.ID, "priority", job.Priority)
			return &job, nil
		}

		// Queue is empty — wait a short interval then retry.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Len returns the number of jobs in the sorted set.
func (q *RedisQueue) Len() int {
	n, err := q.client.ZCard(context.Background(), q.key).Result()
	if err != nil {
		slog.Warn("queue: redis len: zcard", "error", err)
		return 0
	}
	return int(n)
}

// Close releases the underlying Redis client.
func (q *RedisQueue) Close() error {
	return q.client.Close()
}
