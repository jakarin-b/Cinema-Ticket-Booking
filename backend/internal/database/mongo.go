package database

import (
	"context"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readconcern"
	"go.mongodb.org/mongo-driver/mongo/writeconcern"
)

type Store struct {
	Client *mongo.Client
	DB     *mongo.Database
}

func Connect(ctx context.Context, cfg config.Config) (*Store, error) {
	client, err := mongo.Connect(ctx, options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := client.Ping(pingCtx, nil); err != nil {
		_ = client.Disconnect(context.Background())
		return nil, err
	}
	return &Store{Client: client, DB: client.Database(cfg.MongoDatabase)}, nil
}

func (s *Store) Close(ctx context.Context) error { return s.Client.Disconnect(ctx) }

func (s *Store) WithTransaction(ctx context.Context, fn func(mongo.SessionContext) (any, error)) (any, error) {
	session, err := s.Client.StartSession()
	if err != nil {
		return nil, err
	}
	defer session.EndSession(ctx)
	return session.WithTransaction(ctx, fn, options.Transaction().
		SetReadConcern(readconcern.Snapshot()).
		SetWriteConcern(writeconcern.Majority()))
}

func (s *Store) EnsureIndexes(ctx context.Context) error {
	indexes := map[string][]mongo.IndexModel{
		"users": {
			{Keys: bson.D{{Key: "firebase_uid", Value: 1}}, Options: options.Index().SetUnique(true).SetSparse(true).SetName("uniq_firebase_uid")},
			{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_user_email")},
		},
		"auth_identities": {
			{Keys: bson.D{{Key: "provider", Value: 1}, {Key: "subject", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_provider_subject")},
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "provider", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_user_provider")},
		},
		"showtime_seats": {
			{Keys: bson.D{{Key: "showtime_id", Value: 1}, {Key: "seat_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_showtime_seat")},
			{Keys: bson.D{{Key: "showtime_id", Value: 1}, {Key: "status", Value: 1}}, Options: options.Index().SetName("showtime_seat_status")},
		},
		"holds": {
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "idempotency_key", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_hold_idempotency")},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "expires_at", Value: 1}}, Options: options.Index().SetName("expired_holds")},
		},
		"bookings": {
			{Keys: bson.D{{Key: "booking_number", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_booking_number")},
			{Keys: bson.D{{Key: "hold_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_booking_hold")},
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "confirmation_idempotency_key", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_booking_confirmation")},
			{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}, Options: options.Index().SetName("user_bookings")},
			{Keys: bson.D{{Key: "movie_id", Value: 1}, {Key: "showtime_start", Value: 1}, {Key: "booking_status", Value: 1}}, Options: options.Index().SetName("admin_booking_filters")},
		},
		"audit_logs": {
			{Keys: bson.D{{Key: "created_at", Value: -1}, {Key: "event_type", Value: 1}}, Options: options.Index().SetName("audit_listing")},
		},
		"outbox_events": {
			{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_outbox_event")},
			{Keys: bson.D{{Key: "status", Value: 1}, {Key: "next_attempt_at", Value: 1}, {Key: "lease_until", Value: 1}}, Options: options.Index().SetName("outbox_poll")},
		},
		"notifications": {
			{Keys: bson.D{{Key: "event_id", Value: 1}}, Options: options.Index().SetUnique(true).SetName("uniq_notification_event")},
		},
	}
	for collection, models := range indexes {
		if _, err := s.DB.Collection(collection).Indexes().CreateMany(ctx, models); err != nil {
			return err
		}
	}
	return nil
}
