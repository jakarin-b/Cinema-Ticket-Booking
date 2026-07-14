package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	seatlock "github.com/cinema-ticket-booking/backend/internal/lock"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const seatEventChannel = "cinema:seat-events"

var (
	errConditionalSeatMismatch = errors.New("conditional seat update count mismatch")
	errHoldNoLongerConfirmable = errors.New("hold is no longer confirmable")
	errBookingSeatMismatch     = errors.New("conditional booking seat count mismatch")
)

type BookingService struct {
	store   *database.Store
	redis   *redis.Client
	locks   *seatlock.Manager
	cfg     config.Config
	metrics *observability.Metrics
	now     func() time.Time
}

func NewBookingService(store *database.Store, redisClient *redis.Client, locks *seatlock.Manager, cfg config.Config, metrics *observability.Metrics) *BookingService {
	return &BookingService{store: store, redis: redisClient, locks: locks, cfg: cfg, metrics: metrics, now: func() time.Time { return time.Now().UTC() }}
}

type HoldResult struct {
	Hold     domain.Hold `json:"hold"`
	Existing bool        `json:"-"`
}

func (s *BookingService) CreateHold(ctx context.Context, user domain.User, showtimeID primitive.ObjectID, rawSeatIDs []string, idempotencyKey string) (*HoldResult, error) {
	if idempotencyKey == "" {
		return nil, problem(400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required.", nil)
	}
	seatIDs, seatStrings, err := normalizeIDs(rawSeatIDs)
	if err != nil || len(seatIDs) == 0 || len(seatIDs) > 10 {
		return nil, problem(422, "INVALID_SEATS", "Provide between one and ten valid seat IDs.", nil)
	}
	var existing domain.Hold
	if err := s.store.DB.Collection("holds").FindOne(ctx, bson.M{"user_id": user.ID, "idempotency_key": idempotencyKey}).Decode(&existing); err == nil {
		return &HoldResult{Hold: existing, Existing: true}, nil
	} else if err != mongo.ErrNoDocuments {
		return nil, err
	}

	now := s.now()
	var showtime domain.Showtime
	if err := s.store.DB.Collection("showtimes").FindOne(ctx, bson.M{"_id": showtimeID, "status": "ACTIVE", "start_time": bson.M{"$gt": now}}).Decode(&showtime); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, problem(409, "SHOWTIME_UNAVAILABLE", "Showtime is inactive or has already started.", nil)
		}
		return nil, err
	}
	count, err := s.store.DB.Collection("showtime_seats").CountDocuments(ctx, bson.M{"showtime_id": showtimeID, "seat_id": bson.M{"$in": seatIDs}})
	if err != nil {
		return nil, err
	}
	if count != int64(len(seatIDs)) {
		return nil, problem(422, "INVALID_SEATS", "One or more seats do not belong to this showtime.", nil)
	}

	hold := domain.Hold{ID: primitive.NewObjectID(), UserID: user.ID, ShowtimeID: showtimeID, SeatIDs: seatIDs, LockToken: uuid.NewString(), Status: domain.HoldActive, ExpiresAt: now.Add(s.cfg.SeatLockTTL), IdempotencyKey: idempotencyKey, CreatedAt: now, UpdatedAt: now}
	ownership := seatlock.Ownership(hold.ID.Hex(), user.ID.Hex(), hold.LockToken)
	keys := seatlock.Keys(showtimeID.Hex(), seatStrings)
	conflictKey, err := s.locks.Acquire(ctx, keys, ownership)
	if err != nil {
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "redis lock acquisition failed", err)
		return nil, problem(503, "LOCK_SERVICE_UNAVAILABLE", "Seat locking is temporarily unavailable.", nil)
	}
	if conflictKey != "" {
		s.metrics.LockConflicts.Inc()
		s.metrics.Holds.WithLabelValues("conflict").Inc()
		seatID := conflictKey
		if index := strings.LastIndex(conflictKey, ":"); index >= 0 {
			seatID = conflictKey[index+1:]
		}
		return nil, problem(409, "SEAT_UNAVAILABLE", "One or more seats are no longer available.", map[string]any{"seat_ids": []string{seatID}})
	}

	_, txErr := s.store.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		filter := bson.M{"showtime_id": showtimeID, "seat_id": bson.M{"$in": seatIDs}, "$or": bson.A{bson.M{"status": domain.SeatAvailable}, bson.M{"status": domain.SeatLocked, "lock_expires_at": bson.M{"$lte": now}}}}
		update := bson.M{"$set": bson.M{"status": domain.SeatLocked, "hold_id": hold.ID, "locked_by_user_id": user.ID, "lock_expires_at": hold.ExpiresAt, "updated_at": now}, "$inc": bson.M{"version": 1}}
		result, err := s.store.DB.Collection("showtime_seats").UpdateMany(sc, filter, update)
		if err != nil {
			return nil, err
		}
		if result.ModifiedCount != int64(len(seatIDs)) {
			return nil, errConditionalSeatMismatch
		}
		_, err = s.store.DB.Collection("holds").InsertOne(sc, hold)
		return nil, err
	})
	if txErr != nil {
		_, releaseErr := s.locks.Release(context.Background(), keys, ownership)
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "MongoDB hold transaction failed after Redis acquisition", errors.Join(txErr, releaseErr))
		if mongo.IsDuplicateKeyError(txErr) {
			if err := s.store.DB.Collection("holds").FindOne(ctx, bson.M{"user_id": user.ID, "idempotency_key": idempotencyKey}).Decode(&existing); err == nil {
				return &HoldResult{Hold: existing, Existing: true}, nil
			}
		}
		if errors.Is(txErr, errConditionalSeatMismatch) {
			return nil, problem(409, "SEAT_UNAVAILABLE", "One or more seats are no longer available.", map[string]any{"seat_ids": seatStrings})
		}
		return nil, problem(500, "HOLD_CREATION_FAILED", "The hold could not be created.", nil)
	}
	s.metrics.Holds.WithLabelValues("created").Inc()
	s.publishSeatEvent(ctx, "seat.locked", showtimeID, seatStrings, map[string]any{"expires_at": hold.ExpiresAt})
	return &HoldResult{Hold: hold}, nil
}

func (s *BookingService) GetHold(ctx context.Context, user domain.User, id primitive.ObjectID) (*domain.Hold, error) {
	var hold domain.Hold
	err := s.store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": id}).Decode(&hold)
	if err == mongo.ErrNoDocuments {
		return nil, problem(404, "HOLD_NOT_FOUND", "Hold not found.", nil)
	}
	if err != nil {
		return nil, err
	}
	if hold.UserID != user.ID && user.Role != domain.RoleAdmin {
		return nil, problem(403, "FORBIDDEN", "You cannot access this hold.", nil)
	}
	return &hold, nil
}

func (s *BookingService) ReleaseHold(ctx context.Context, user domain.User, holdID primitive.ObjectID) (*domain.Hold, error) {
	hold, err := s.GetHold(ctx, user, holdID)
	if err != nil {
		return nil, err
	}
	if hold.UserID != user.ID {
		return nil, problem(403, "FORBIDDEN", "Only the hold owner may release it.", nil)
	}
	if hold.Status != domain.HoldActive {
		return hold, nil
	}
	now := s.now()
	_, err = s.store.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		res, err := s.store.DB.Collection("holds").UpdateOne(sc, bson.M{"_id": hold.ID, "user_id": user.ID, "status": domain.HoldActive}, bson.M{"$set": bson.M{"status": domain.HoldReleased, "updated_at": now}})
		if err != nil {
			return nil, err
		}
		if res.ModifiedCount != 1 {
			return nil, errors.New("hold is no longer active")
		}
		_, err = s.store.DB.Collection("showtime_seats").UpdateMany(sc, bson.M{"showtime_id": hold.ShowtimeID, "seat_id": bson.M{"$in": hold.SeatIDs}, "status": domain.SeatLocked, "hold_id": hold.ID}, bson.M{"$set": bson.M{"status": domain.SeatAvailable, "updated_at": now}, "$unset": bson.M{"hold_id": "", "locked_by_user_id": "", "lock_expires_at": ""}, "$inc": bson.M{"version": 1}})
		if err != nil {
			return nil, err
		}
		audit := domain.AuditLog{ID: primitive.NewObjectID(), EventType: "SEAT_RELEASED", ActorUserID: &user.ID, EntityType: "hold", EntityID: hold.ID.Hex(), Metadata: map[string]any{"reason": "manual", "seat_ids": objectIDStrings(hold.SeatIDs)}, Severity: "INFO", CreatedAt: now}
		_, err = s.store.DB.Collection("audit_logs").InsertOne(sc, audit)
		return nil, err
	})
	if err != nil {
		var current domain.Hold
		if s.store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": hold.ID}).Decode(&current) == nil && current.Status != domain.HoldActive {
			return &current, nil
		}
		return nil, err
	}
	hold.Status = domain.HoldReleased
	hold.UpdatedAt = now
	keys := seatlock.Keys(hold.ShowtimeID.Hex(), objectIDStrings(hold.SeatIDs))
	ownership := seatlock.Ownership(hold.ID.Hex(), user.ID.Hex(), hold.LockToken)
	if _, err := s.locks.Release(ctx, keys, ownership); err != nil {
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "safe Redis release failed", err)
	}
	s.metrics.Holds.WithLabelValues("released").Inc()
	s.publishSeatEvent(ctx, "seat.released", hold.ShowtimeID, objectIDStrings(hold.SeatIDs), nil)
	return hold, nil
}

func (s *BookingService) Confirm(ctx context.Context, user domain.User, holdID primitive.ObjectID, idempotencyKey, paymentMethod string) (*domain.Booking, bool, error) {
	if idempotencyKey == "" {
		return nil, false, problem(400, "IDEMPOTENCY_KEY_REQUIRED", "Idempotency-Key header is required.", nil)
	}
	if paymentMethod != "MOCK" {
		return nil, false, problem(422, "INVALID_PAYMENT_METHOD", "Only MOCK payment is supported.", nil)
	}
	var existing domain.Booking
	if err := s.store.DB.Collection("bookings").FindOne(ctx, bson.M{"user_id": user.ID, "confirmation_idempotency_key": idempotencyKey}).Decode(&existing); err == nil {
		return &existing, true, nil
	} else if err != mongo.ErrNoDocuments {
		return nil, false, err
	}
	var hold domain.Hold
	if err := s.store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": holdID}).Decode(&hold); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, false, problem(404, "HOLD_NOT_FOUND", "Hold not found.", nil)
		}
		return nil, false, err
	}
	if hold.UserID != user.ID {
		return nil, false, problem(403, "FORBIDDEN", "Only the hold owner may confirm it.", nil)
	}
	now := s.now()
	if hold.Status != domain.HoldActive || !now.Before(hold.ExpiresAt) {
		return nil, false, problem(409, "HOLD_EXPIRED", "The hold is no longer active.", nil)
	}
	keys := seatlock.Keys(hold.ShowtimeID.Hex(), objectIDStrings(hold.SeatIDs))
	ownership := seatlock.Ownership(hold.ID.Hex(), user.ID.Hex(), hold.LockToken)
	if mismatch, err := s.locks.Verify(ctx, keys, ownership); err != nil {
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "Redis ownership verification failed", err)
		return nil, false, problem(503, "LOCK_SERVICE_UNAVAILABLE", "Seat locking is temporarily unavailable.", nil)
	} else if mismatch != "" {
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "unexpected lock ownership mismatch", fmt.Errorf("key %s", mismatch))
		return nil, false, problem(409, "HOLD_EXPIRED", "The seat lock is no longer owned by this hold.", nil)
	}

	var seats []domain.ShowtimeSeat
	cur, err := s.store.DB.Collection("showtime_seats").Find(ctx, bson.M{"showtime_id": hold.ShowtimeID, "seat_id": bson.M{"$in": hold.SeatIDs}})
	if err != nil {
		return nil, false, err
	}
	if err := cur.All(ctx, &seats); err != nil {
		return nil, false, err
	}
	_ = cur.Close(ctx)
	if len(seats) != len(hold.SeatIDs) {
		return nil, false, problem(409, "HOLD_INVALID", "Held seats could not be resolved.", nil)
	}
	var show domain.Showtime
	if err := s.store.DB.Collection("showtimes").FindOne(ctx, bson.M{"_id": hold.ShowtimeID}).Decode(&show); err != nil {
		return nil, false, err
	}
	var movie domain.Movie
	if err := s.store.DB.Collection("movies").FindOne(ctx, bson.M{"_id": show.MovieID}).Decode(&movie); err != nil {
		return nil, false, err
	}
	var auditorium domain.Auditorium
	if err := s.store.DB.Collection("auditoriums").FindOne(ctx, bson.M{"_id": show.AuditoriumID}).Decode(&auditorium); err != nil {
		return nil, false, err
	}
	bookingSeats := make([]domain.BookingSeat, 0, len(seats))
	var total int64
	for _, seat := range seats {
		bookingSeats = append(bookingSeats, domain.BookingSeat{SeatID: seat.SeatID, Label: seat.SeatLabel, Price: seat.Price})
		total += seat.Price
	}
	sort.Slice(bookingSeats, func(i, j int) bool { return bookingSeats[i].Label < bookingSeats[j].Label })
	booking := domain.Booking{ID: primitive.NewObjectID(), BookingNumber: "BKG-" + now.Format("20060102") + "-" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8]), HoldID: hold.ID, UserID: user.ID, UserEmail: user.Email, ShowtimeID: show.ID, MovieID: movie.ID, MovieTitle: movie.Title, ShowtimeStart: show.StartTime, AuditoriumName: auditorium.Name, Seats: bookingSeats, TotalAmount: total, Currency: "THB", PaymentStatus: "PAID", BookingStatus: "CONFIRMED", ConfirmationIdempotencyKey: idempotencyKey, CreatedAt: now, UpdatedAt: now}
	eventID := uuid.NewString()
	seatStrings := objectIDStrings(hold.SeatIDs)
	_, txErr := s.store.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		res, err := s.store.DB.Collection("holds").UpdateOne(sc, bson.M{"_id": hold.ID, "user_id": user.ID, "status": domain.HoldActive, "expires_at": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"status": domain.HoldConfirmed, "updated_at": now}})
		if err != nil {
			return nil, err
		}
		if res.ModifiedCount != 1 {
			return nil, errHoldNoLongerConfirmable
		}
		res, err = s.store.DB.Collection("showtime_seats").UpdateMany(sc, bson.M{"showtime_id": hold.ShowtimeID, "seat_id": bson.M{"$in": hold.SeatIDs}, "status": domain.SeatLocked, "hold_id": hold.ID, "locked_by_user_id": user.ID, "lock_expires_at": bson.M{"$gt": now}}, bson.M{"$set": bson.M{"status": domain.SeatBooked, "booking_id": booking.ID, "updated_at": now}, "$unset": bson.M{"hold_id": "", "locked_by_user_id": "", "lock_expires_at": ""}, "$inc": bson.M{"version": 1}})
		if err != nil {
			return nil, err
		}
		if res.ModifiedCount != int64(len(hold.SeatIDs)) {
			return nil, errBookingSeatMismatch
		}
		if _, err = s.store.DB.Collection("bookings").InsertOne(sc, booking); err != nil {
			return nil, err
		}
		audit := domain.AuditLog{ID: primitive.NewObjectID(), EventType: "BOOKING_SUCCESS", ActorUserID: &user.ID, EntityType: "booking", EntityID: booking.ID.Hex(), Metadata: map[string]any{"booking_number": booking.BookingNumber, "seat_ids": seatStrings}, Severity: "INFO", CreatedAt: now}
		if _, err = s.store.DB.Collection("audit_logs").InsertOne(sc, audit); err != nil {
			return nil, err
		}
		outbox := domain.OutboxEvent{ID: primitive.NewObjectID(), EventID: eventID, EventType: "booking.confirmed", Payload: map[string]any{"event_id": eventID, "event_type": "booking.confirmed", "occurred_at": now, "booking_id": booking.ID.Hex(), "user_id": user.ID.Hex(), "showtime_id": show.ID.Hex(), "seat_ids": seatStrings}, Status: "PENDING", Attempts: 0, NextAttemptAt: now, CreatedAt: now}
		_, err = s.store.DB.Collection("outbox_events").InsertOne(sc, outbox)
		return nil, err
	})
	if txErr != nil {
		if mongo.IsDuplicateKeyError(txErr) {
			if err := s.store.DB.Collection("bookings").FindOne(ctx, bson.M{"$or": bson.A{bson.M{"hold_id": hold.ID}, bson.M{"user_id": user.ID, "confirmation_idempotency_key": idempotencyKey}}}).Decode(&existing); err == nil {
				return &existing, true, nil
			}
		}
		if errors.Is(txErr, errHoldNoLongerConfirmable) || errors.Is(txErr, errBookingSeatMismatch) {
			return nil, false, problem(409, "HOLD_EXPIRED", "The hold could not be confirmed.", nil)
		}
		s.auditSystemError(ctx, user.ID, "hold", hold.ID.Hex(), "booking confirmation transaction failed", txErr)
		return nil, false, problem(500, "BOOKING_CONFIRMATION_FAILED", "The booking could not be confirmed.", nil)
	}
	if _, err := s.locks.Release(ctx, keys, ownership); err != nil {
		s.auditSystemError(ctx, user.ID, "booking", booking.ID.Hex(), "safe Redis cleanup after booking failed", err)
	}
	s.metrics.Bookings.Inc()
	s.publishSeatEvent(ctx, "seat.booked", show.ID, seatStrings, map[string]any{"booking_id": booking.ID.Hex()})
	return &booking, false, nil
}

func (s *BookingService) MyBookings(ctx context.Context, userID primitive.ObjectID, page, limit int64) ([]domain.Booking, int64, error) {
	filter := bson.M{"user_id": userID}
	return s.listBookings(ctx, filter, page, limit)
}

func (s *BookingService) Booking(ctx context.Context, user domain.User, id primitive.ObjectID) (*domain.Booking, error) {
	var b domain.Booking
	err := s.store.DB.Collection("bookings").FindOne(ctx, bson.M{"_id": id}).Decode(&b)
	if err == mongo.ErrNoDocuments {
		return nil, problem(404, "BOOKING_NOT_FOUND", "Booking not found.", nil)
	}
	if err != nil {
		return nil, err
	}
	if b.UserID != user.ID && user.Role != domain.RoleAdmin {
		return nil, problem(403, "FORBIDDEN", "You cannot access this booking.", nil)
	}
	return &b, nil
}

func (s *BookingService) ExpireDueHolds(ctx context.Context, limit int64) (int, error) {
	now := s.now()
	cur, err := s.store.DB.Collection("holds").Find(ctx, bson.M{"status": domain.HoldActive, "expires_at": bson.M{"$lte": now}}, options.Find().SetLimit(limit).SetSort(bson.D{{Key: "expires_at", Value: 1}}))
	if err != nil {
		return 0, err
	}
	defer cur.Close(ctx)
	var holds []domain.Hold
	if err := cur.All(ctx, &holds); err != nil {
		return 0, err
	}
	processed := 0
	for _, hold := range holds {
		ok, err := s.expireOne(ctx, hold, now)
		if err != nil {
			s.auditSystemError(ctx, hold.UserID, "hold", hold.ID.Hex(), "worker expiration failed", err)
			continue
		}
		if ok {
			processed++
		}
	}
	return processed, nil
}

func (s *BookingService) expireOne(ctx context.Context, hold domain.Hold, now time.Time) (bool, error) {
	result, err := s.store.WithTransaction(ctx, func(sc mongo.SessionContext) (any, error) {
		res, err := s.store.DB.Collection("holds").UpdateOne(sc, bson.M{"_id": hold.ID, "status": domain.HoldActive, "expires_at": bson.M{"$lte": now}}, bson.M{"$set": bson.M{"status": domain.HoldExpired, "updated_at": now}})
		if err != nil {
			return nil, err
		}
		if res.ModifiedCount == 0 {
			return false, nil
		}
		_, err = s.store.DB.Collection("showtime_seats").UpdateMany(sc, bson.M{"showtime_id": hold.ShowtimeID, "seat_id": bson.M{"$in": hold.SeatIDs}, "status": domain.SeatLocked, "hold_id": hold.ID}, bson.M{"$set": bson.M{"status": domain.SeatAvailable, "updated_at": now}, "$unset": bson.M{"hold_id": "", "locked_by_user_id": "", "lock_expires_at": ""}, "$inc": bson.M{"version": 1}})
		if err != nil {
			return nil, err
		}
		audits := []any{domain.AuditLog{ID: primitive.NewObjectID(), EventType: "BOOKING_TIMEOUT", ActorUserID: &hold.UserID, EntityType: "hold", EntityID: hold.ID.Hex(), Severity: "INFO", CreatedAt: now}, domain.AuditLog{ID: primitive.NewObjectID(), EventType: "SEAT_RELEASED", ActorUserID: &hold.UserID, EntityType: "hold", EntityID: hold.ID.Hex(), Metadata: map[string]any{"reason": "timeout", "seat_ids": objectIDStrings(hold.SeatIDs)}, Severity: "INFO", CreatedAt: now}}
		_, err = s.store.DB.Collection("audit_logs").InsertMany(sc, audits)
		return true, err
	})
	if err != nil {
		return false, err
	}
	claimed, ok := result.(bool)
	if !ok || !claimed {
		return false, nil
	}
	var current domain.Hold
	if err := s.store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": hold.ID}).Decode(&current); err != nil || current.Status != domain.HoldExpired {
		return false, err
	}
	keys := seatlock.Keys(hold.ShowtimeID.Hex(), objectIDStrings(hold.SeatIDs))
	ownership := seatlock.Ownership(hold.ID.Hex(), hold.UserID.Hex(), hold.LockToken)
	_, _ = s.locks.Release(ctx, keys, ownership)
	s.metrics.ExpiredHolds.Inc()
	s.publishSeatEvent(ctx, "seat.released", hold.ShowtimeID, objectIDStrings(hold.SeatIDs), map[string]any{"reason": "timeout"})
	return true, nil
}

func (s *BookingService) listBookings(ctx context.Context, filter bson.M, page, limit int64) ([]domain.Booking, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	total, err := s.store.DB.Collection("bookings").CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	cur, err := s.store.DB.Collection("bookings").Find(ctx, filter, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetSkip((page-1)*limit).SetLimit(limit))
	if err != nil {
		return nil, 0, err
	}
	defer cur.Close(ctx)
	var out []domain.Booking
	if err := cur.All(ctx, &out); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func (s *BookingService) publishSeatEvent(ctx context.Context, eventType string, showtimeID primitive.ObjectID, seatIDs []string, extra map[string]any) {
	data := map[string]any{"seat_ids": seatIDs}
	for k, v := range extra {
		data[k] = v
	}
	event := domain.SeatEvent{EventID: uuid.NewString(), Type: eventType, ShowtimeID: showtimeID.Hex(), OccurredAt: s.now(), Data: data}
	payload, _ := json.Marshal(event)
	if err := s.redis.Publish(ctx, seatEventChannel, payload).Err(); err != nil {
		slog.Error("seat event publish failed", "error", err, "event_type", eventType, "showtime_id", showtimeID.Hex())
	}
}

func (s *BookingService) auditSystemError(ctx context.Context, actor primitive.ObjectID, entityType, entityID, message string, err error) {
	metadata := map[string]any{"message": message}
	if err != nil {
		metadata["error"] = err.Error()
	}
	audit := domain.AuditLog{ID: primitive.NewObjectID(), EventType: "SYSTEM_ERROR", ActorUserID: &actor, EntityType: entityType, EntityID: entityID, Metadata: metadata, Severity: "ERROR", CreatedAt: s.now()}
	if _, insertErr := s.store.DB.Collection("audit_logs").InsertOne(ctx, audit); insertErr != nil {
		slog.Error("system audit insert failed", "error", insertErr, "original_error", err)
	}
}

func normalizeIDs(raw []string) ([]primitive.ObjectID, []string, error) {
	seen := map[string]struct{}{}
	var ids []primitive.ObjectID
	var stringsOut []string
	for _, value := range raw {
		value = strings.TrimSpace(value)
		id, err := primitive.ObjectIDFromHex(value)
		if err != nil {
			return nil, nil, err
		}
		normalized := id.Hex()
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		ids = append(ids, id)
		stringsOut = append(stringsOut, normalized)
	}
	sort.Strings(stringsOut)
	ids = ids[:0]
	for _, value := range stringsOut {
		id, _ := primitive.ObjectIDFromHex(value)
		ids = append(ids, id)
	}
	return ids, stringsOut, nil
}

func objectIDStrings(ids []primitive.ObjectID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Hex()
	}
	sort.Strings(out)
	return out
}
