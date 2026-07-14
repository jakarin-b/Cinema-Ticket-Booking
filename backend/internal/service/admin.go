package service

import (
	"context"
	"regexp"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type AdminService struct{ store *database.Store }

func NewAdminService(store *database.Store) *AdminService { return &AdminService{store: store} }

type BookingFilters struct {
	MovieID       string
	DateFrom      string
	DateTo        string
	UserEmail     string
	BookingStatus string
}

func (s *AdminService) Bookings(ctx context.Context, filters BookingFilters, page, limit int64) ([]domain.Booking, int64, error) {
	filter := bson.M{}
	if filters.MovieID != "" {
		id, err := primitive.ObjectIDFromHex(filters.MovieID)
		if err != nil {
			return nil, 0, problem(422, "INVALID_FILTER", "movie_id is invalid.", nil)
		}
		filter["movie_id"] = id
	}
	if filters.UserEmail != "" {
		filter["user_email"] = bson.M{"$regex": regexp.QuoteMeta(filters.UserEmail), "$options": "i"}
	}
	if filters.BookingStatus != "" {
		filter["booking_status"] = filters.BookingStatus
	}
	timeFilter := bson.M{}
	if filters.DateFrom != "" {
		t, err := time.Parse(time.RFC3339, filters.DateFrom)
		if err != nil {
			return nil, 0, problem(422, "INVALID_FILTER", "date_from must be RFC3339.", nil)
		}
		timeFilter["$gte"] = t.UTC()
	}
	if filters.DateTo != "" {
		t, err := time.Parse(time.RFC3339, filters.DateTo)
		if err != nil {
			return nil, 0, problem(422, "INVALID_FILTER", "date_to must be RFC3339.", nil)
		}
		timeFilter["$lte"] = t.UTC()
	}
	if len(timeFilter) > 0 {
		filter["showtime_start"] = timeFilter
	}
	b := &BookingService{store: s.store}
	return b.listBookings(ctx, filter, page, limit)
}

type AuditFilters struct {
	EventType string
	Severity  string
}

func (s *AdminService) AuditLogs(ctx context.Context, filters AuditFilters, page, limit int64) ([]domain.AuditLog, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	filter := bson.M{}
	if filters.EventType != "" {
		filter["event_type"] = filters.EventType
	}
	if filters.Severity != "" {
		filter["severity"] = filters.Severity
	}
	total, err := s.store.DB.Collection("audit_logs").CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cur, err := s.store.DB.Collection("audit_logs").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetSkip((page-1)*limit).SetLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	var out []domain.AuditLog
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}
