package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/cinema-ticket-booking/backend/internal/messaging"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

type Processor struct {
	store   *database.Store
	booking *service.BookingService
	rabbit  *messaging.Rabbit
	cfg     config.Config
	metrics *observability.Metrics
}

func NewProcessor(store *database.Store, booking *service.BookingService, rabbit *messaging.Rabbit, cfg config.Config, metrics *observability.Metrics) *Processor {
	return &Processor{store: store, booking: booking, rabbit: rabbit, cfg: cfg, metrics: metrics}
}

func (p *Processor) RunExpiration(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.HoldSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			runCtx, span := otel.Tracer("cinema-worker").Start(ctx, "holds.expiration_sweep")
			count, err := p.booking.ExpireDueHolds(runCtx, 100)
			span.SetAttributes(attribute.Int("holds.expired", count))
			if err != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, "expiration sweep failed")
				slog.Error("expiration sweep failed", "error", err)
			} else if count > 0 {
				slog.Info("expired holds processed", "count", count)
			}
			span.End()
		}
	}
}

func (p *Processor) RunOutbox(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.updateOutboxLag(ctx)
			for i := 0; i < 20; i++ {
				event, err := p.claimOutbox(ctx)
				if err == mongo.ErrNoDocuments {
					break
				}
				if err != nil {
					slog.Error("outbox claim failed", "error", err)
					break
				}
				publishCtx, span := otel.Tracer("cinema-worker").Start(ctx, "outbox.publish", traceEventAttributes(event)...)
				if err := p.publishOutbox(publishCtx, event); err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, "outbox publish failed")
					p.metrics.OutboxFailures.Inc()
					slog.Error("outbox publish failed", "event_id", event.EventID, "error", err)
				}
				span.End()
			}
		}
	}
}

func (p *Processor) claimOutbox(ctx context.Context) (*domain.OutboxEvent, error) {
	now := time.Now().UTC()
	filter := bson.M{"next_attempt_at": bson.M{"$lte": now}, "$or": bson.A{bson.M{"status": "PENDING"}, bson.M{"status": "PROCESSING", "lease_until": bson.M{"$lte": now}}}}
	update := bson.M{"$set": bson.M{"status": "PROCESSING", "lease_until": now.Add(30 * time.Second)}, "$inc": bson.M{"attempts": 1}}
	var event domain.OutboxEvent
	err := p.store.DB.Collection("outbox_events").FindOneAndUpdate(ctx, filter, update, options.FindOneAndUpdate().SetSort(bson.D{{Key: "next_attempt_at", Value: 1}}).SetReturnDocument(options.After)).Decode(&event)
	return &event, err
}

func (p *Processor) publishOutbox(ctx context.Context, event *domain.OutboxEvent) error {
	payload, _ := json.Marshal(event.Payload)
	publishCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := p.rabbit.Publish(publishCtx, event.EventType, payload, amqp091.Table{"x-event-id": event.EventID})
	now := time.Now().UTC()
	if err == nil {
		_, updateErr := p.store.DB.Collection("outbox_events").UpdateOne(ctx, bson.M{"_id": event.ID, "status": "PROCESSING"}, bson.M{"$set": bson.M{"status": "PUBLISHED", "published_at": now}, "$unset": bson.M{"lease_until": "", "last_error": ""}})
		if updateErr == nil {
			p.metrics.OutboxPublished.Inc()
		}
		return updateErr
	}
	backoff := time.Duration(math.Min(math.Pow(2, float64(event.Attempts)), 60)) * time.Second
	_, _ = p.store.DB.Collection("outbox_events").UpdateOne(ctx, bson.M{"_id": event.ID}, bson.M{"$set": bson.M{"status": "PENDING", "next_attempt_at": now.Add(backoff), "last_error": err.Error()}, "$unset": bson.M{"lease_until": ""}})
	return err
}

func (p *Processor) RunNotifications(ctx context.Context) error {
	deliveries, ch, err := p.rabbit.Consume()
	if err != nil {
		return err
	}
	defer ch.Close()
	for {
		select {
		case <-ctx.Done():
			return nil
		case delivery, ok := <-deliveries:
			if !ok {
				return fmt.Errorf("notification delivery channel closed")
			}
			deliveryCtx, span := otel.Tracer("cinema-worker").Start(ctx, "notification.consume")
			retryCount := headerInt(delivery.Headers["x-retry-count"])
			span.SetAttributes(attribute.Int("messaging.retry_count", retryCount))
			if err := p.notify(deliveryCtx, delivery.Body); err == nil {
				_ = delivery.Ack(false)
				span.End()
				continue
			} else {
				span.RecordError(err)
			}
			retry := retryCount + 1
			if retry <= 3 {
				headers := delivery.Headers
				if headers == nil {
					headers = amqp091.Table{}
				}
				headers["x-retry-count"] = int32(retry)
				if err := p.rabbit.PublishRetry(deliveryCtx, retry, delivery.Body, headers); err == nil {
					p.metrics.NotificationRetries.Inc()
					_ = delivery.Ack(false)
					span.SetAttributes(attribute.Int("messaging.next_retry", retry))
					span.End()
					continue
				} else {
					span.RecordError(err)
				}
			}
			p.metrics.DLQMessages.Inc()
			_ = delivery.Nack(false, false)
			span.SetStatus(codes.Error, "notification dead-lettered")
			span.End()
		}
	}
}

func (p *Processor) updateOutboxLag(ctx context.Context) {
	var event domain.OutboxEvent
	err := p.store.DB.Collection("outbox_events").FindOne(
		ctx,
		bson.M{"status": bson.M{"$in": bson.A{"PENDING", "PROCESSING"}}},
		options.FindOne().SetSort(bson.D{{Key: "created_at", Value: 1}}).SetProjection(bson.M{"created_at": 1}),
	).Decode(&event)
	if err == mongo.ErrNoDocuments {
		p.metrics.OutboxLag.Set(0)
		return
	}
	if err != nil {
		slog.Warn("outbox lag query failed", "error", err)
		return
	}
	lag := time.Since(event.CreatedAt).Seconds()
	if lag < 0 {
		lag = 0
	}
	p.metrics.OutboxLag.Set(lag)
}

func traceEventAttributes(event *domain.OutboxEvent) []trace.SpanStartOption {
	return []trace.SpanStartOption{trace.WithAttributes(
		attribute.String("messaging.message.id", event.EventID),
		attribute.String("messaging.destination.name", event.EventType),
		attribute.Int("messaging.message.retry_count", event.Attempts),
	)}
}

func (p *Processor) notify(ctx context.Context, payload []byte) error {
	var event struct {
		EventID   string `json:"event_id"`
		BookingID string `json:"booking_id"`
	}
	if err := json.Unmarshal(payload, &event); err != nil {
		return err
	}
	if event.EventID == "" {
		return fmt.Errorf("event_id is required")
	}
	bookingID, err := primitive.ObjectIDFromHex(event.BookingID)
	if err != nil {
		return err
	}
	var booking domain.Booking
	if err := p.store.DB.Collection("bookings").FindOne(ctx, bson.M{"_id": bookingID}).Decode(&booking); err != nil {
		return err
	}
	labels := make([]string, len(booking.Seats))
	for i, seat := range booking.Seats {
		labels[i] = seat.Label
	}
	message := fmt.Sprintf("Booking %s confirmed for %s; %s at %s; seats %v", booking.BookingNumber, booking.UserEmail, booking.MovieTitle, booking.ShowtimeStart.Format(time.RFC3339), labels)
	notification := domain.Notification{ID: primitive.NewObjectID(), EventID: event.EventID, BookingID: booking.ID, Message: message, CreatedAt: time.Now().UTC()}
	if _, err := p.store.DB.Collection("notifications").InsertOne(ctx, notification); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	slog.Info("mock booking notification", "event_id", event.EventID, "booking_number", booking.BookingNumber, "user", booking.UserEmail, "movie", booking.MovieTitle, "showtime", booking.ShowtimeStart, "seats", labels)
	return nil
}

func headerInt(value any) int {
	switch x := value.(type) {
	case int32:
		return int(x)
	case int64:
		return int(x)
	case int:
		return x
	default:
		return 0
	}
}
