package redis

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/zeotap/ims/internal/models"
)

const (
	liveFeedKey    = "live:active"  // sorted set: active incidents, score = severity (0=P0)
	closedFeedKey  = "live:closed"  // sorted set: closed incidents, score = -unix ts (newest first)
	incidentPrefix = "incident:"
	sseChannel     = "sse:events"
)

type Store struct {
	client *redis.Client
}

func New(addr, password string) (*Store, error) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           0,
		PoolSize:     20,
		MinIdleConns: 5,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}
	return &Store{client: rdb}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx).Err()
}

func (s *Store) Close() error { return s.client.Close() }

// UpsertIncident stores the work item and maintains two sorted sets:
//   - live:active  — non-closed, sorted by severity (P0=0 first)
//   - live:closed  — closed,     sorted by last_signal_at descending (newest first)
func (s *Store) UpsertIncident(ctx context.Context, wi *models.WorkItem) error {
	data, err := json.Marshal(wi)
	if err != nil {
		return err
	}

	pipe := s.client.Pipeline()
	pipe.Set(ctx, incidentPrefix+wi.ID.String(), data, 24*time.Hour)

	if wi.Status == models.StatusClosed {
		// Remove from active, add to closed (score = negative unix so newest = lowest score = first in ZRange)
		pipe.ZRem(ctx, liveFeedKey, wi.ID.String())
		pipe.ZAdd(ctx, closedFeedKey, redis.Z{
			Score:  -float64(wi.LastSignalAt.Unix()),
			Member: wi.ID.String(),
		})
	} else {
		// Remove from closed (re-opened edge case), add/update in active
		pipe.ZRem(ctx, closedFeedKey, wi.ID.String())
		pipe.ZAdd(ctx, liveFeedKey, redis.Z{
			Score:  severityScore(wi.Severity),
			Member: wi.ID.String(),
		})
	}
	_, err = pipe.Exec(ctx)
	return err
}

// GetAllIncidents returns active incidents (severity-sorted) followed by closed
// incidents (newest-first) from Redis — the hot-path dashboard read.
func (s *Store) GetAllIncidents(ctx context.Context, limit int) ([]*models.WorkItem, error) {
	activeIDs, err := s.client.ZRange(ctx, liveFeedKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	closedIDs, err := s.client.ZRange(ctx, closedFeedKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	all := append(activeIDs, closedIDs...)
	if len(all) == 0 {
		return nil, fmt.Errorf("cache miss")
	}
	return s.hydrate(ctx, all)
}

// GetLiveFeed returns only active incidents (kept for SSE context / internal use).
func (s *Store) GetLiveFeed(ctx context.Context, limit int) ([]*models.WorkItem, error) {
	ids, err := s.client.ZRange(ctx, liveFeedKey, 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []*models.WorkItem{}, nil
	}
	return s.hydrate(ctx, ids)
}

// hydrate fetches work item payloads for a list of IDs via MGet.
func (s *Store) hydrate(ctx context.Context, ids []string) ([]*models.WorkItem, error) {
	if len(ids) == 0 {
		return []*models.WorkItem{}, nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = incidentPrefix + id
	}
	vals, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}
	items := make([]*models.WorkItem, 0, len(vals))
	for _, v := range vals {
		if v == nil {
			continue
		}
		var wi models.WorkItem
		if err := json.Unmarshal([]byte(v.(string)), &wi); err == nil {
			items = append(items, &wi)
		}
	}
	return items, nil
}

// GetIncident returns a cached work item.
func (s *Store) GetIncident(ctx context.Context, id string) (*models.WorkItem, error) {
	val, err := s.client.Get(ctx, incidentPrefix+id).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var wi models.WorkItem
	if err := json.Unmarshal([]byte(val), &wi); err != nil {
		return nil, err
	}
	return &wi, nil
}

// Publish emits a JSON-encoded SSE event to all subscribers.
func (s *Store) Publish(ctx context.Context, event *models.SSEEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return s.client.Publish(ctx, sseChannel, data).Err()
}

// Subscribe returns a Redis PubSub for the SSE channel.
func (s *Store) Subscribe(ctx context.Context) *redis.PubSub {
	return s.client.Subscribe(ctx, sseChannel)
}

func severityScore(s models.Severity) float64 {
	switch s {
	case models.SeverityP0:
		return 0
	case models.SeverityP1:
		return 1
	case models.SeverityP2:
		return 2
	default:
		return 3
	}
}
