package mongo

import (
	"context"
	"fmt"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"github.com/zeotap/ims/internal/models"
	"github.com/zeotap/ims/internal/resilience"
)

type Store struct {
	client  *mongo.Client
	signals *mongo.Collection
	breaker interface{ Execute(func() (interface{}, error)) (interface{}, error) }
}

func New(uri string) (*Store, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOpts := options.Client().ApplyURI(uri).
		SetMaxPoolSize(20).
		SetMinPoolSize(5)

	client, err := mongo.Connect(ctx, clientOpts)
	if err != nil {
		return nil, fmt.Errorf("mongo connect: %w", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		return nil, fmt.Errorf("mongo ping: %w", err)
	}

	coll := client.Database("ims").Collection("raw_signals")

	// Ensure indexes
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "component_id", Value: 1}, {Key: "timestamp", Value: -1}}},
		{Keys: bson.D{{Key: "work_item_id", Value: 1}}},
		{Keys: bson.D{{Key: "timestamp", Value: -1}}},
	}
	if _, err := coll.Indexes().CreateMany(ctx, indexes); err != nil {
		return nil, fmt.Errorf("mongo index: %w", err)
	}

	return &Store{
		client:  client,
		signals: coll,
		breaker: resilience.NewBreaker("mongo"),
	}, nil
}

func (s *Store) Ping(ctx context.Context) error {
	return s.client.Ping(ctx, nil)
}

func (s *Store) Close(ctx context.Context) error {
	return s.client.Disconnect(ctx)
}

// InsertSignal stores a raw signal. Fire-and-forget friendly; caller uses retry externally.
func (s *Store) InsertSignal(ctx context.Context, sig *models.Signal) error {
	return resilience.Retry(ctx, 5, func() error {
		sig.ReceivedAt = time.Now()
		_, err := s.signals.InsertOne(ctx, sig)
		return err
	})
}

// BulkInsertSignals uses an ordered=false bulk write for throughput.
func (s *Store) BulkInsertSignals(ctx context.Context, sigs []*models.Signal) error {
	if len(sigs) == 0 {
		return nil
	}
	docs := make([]interface{}, len(sigs))
	now := time.Now()
	for i, s := range sigs {
		s.ReceivedAt = now
		docs[i] = s
	}
	return resilience.Retry(ctx, 5, func() error {
		_, err := s.signals.InsertMany(ctx, docs, options.InsertMany().SetOrdered(false))
		return err
	})
}

// GetSignalsByWorkItem returns raw signals linked to a work item (paginated).
func (s *Store) GetSignalsByWorkItem(ctx context.Context, workItemID string, limit, skip int64) ([]*models.Signal, error) {
	filter := bson.M{"work_item_id": workItemID}
	opts := options.Find().
		SetSort(bson.D{{Key: "timestamp", Value: -1}}).
		SetLimit(limit).
		SetSkip(skip)

	cursor, err := s.signals.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*models.Signal
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

// GetSignalsByComponent returns recent signals for a component (for the live feed detail).
func (s *Store) GetSignalsByComponent(ctx context.Context, componentID string, since time.Time) ([]*models.Signal, error) {
	filter := bson.M{
		"component_id": componentID,
		"timestamp":    bson.M{"$gte": primitive.NewDateTimeFromTime(since)},
	}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}}).SetLimit(100)
	cursor, err := s.signals.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []*models.Signal
	return results, cursor.All(ctx, &results)
}

// UpdateWorkItemID links signals matching component+time-range to a work item.
func (s *Store) UpdateWorkItemID(ctx context.Context, componentID string, since time.Time, workItemID string) error {
	_, err := s.signals.UpdateMany(ctx,
		bson.M{"component_id": componentID, "timestamp": bson.M{"$gte": primitive.NewDateTimeFromTime(since)}, "work_item_id": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"work_item_id": workItemID}},
	)
	return err
}
