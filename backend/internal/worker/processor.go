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
	"github.com/cinema-ticket-booking/backend/internal/mailer"
	"github.com/cinema-ticket-booking/backend/internal/messaging"
	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/rabbitmq/amqp091-go"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type Processor struct {
	store   *database.Store
	booking *service.BookingService
	rabbit  *messaging.Rabbit
	sender  mailer.Sender
	cfg     config.Config
}

func NewProcessor(store *database.Store, booking *service.BookingService, rabbit *messaging.Rabbit, sender mailer.Sender, cfg config.Config) *Processor {
	return &Processor{store: store, booking: booking, rabbit: rabbit, sender: sender, cfg: cfg}
}

func (p *Processor) RunExpiration(ctx context.Context) {
	ticker := time.NewTicker(p.cfg.HoldSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			count, err := p.booking.ExpireDueHolds(ctx, 100)
			if err != nil {
				slog.Error("expiration sweep failed", "error", err)
			} else if count > 0 {
				slog.Info("expired holds processed", "count", count)
			}
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
			for i := 0; i < 20; i++ {
				event, err := p.claimOutbox(ctx)
				if err == mongo.ErrNoDocuments {
					break
				}
				if err != nil {
					slog.Error("outbox claim failed", "error", err)
					break
				}
				if err := p.publishOutbox(ctx, event); err != nil {
					slog.Error("outbox publish failed", "event_id", event.EventID, "error", err)
				}
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
			retryCount := headerInt(delivery.Headers["x-retry-count"])
			if err := p.notify(ctx, delivery.Body); err == nil {
				_ = delivery.Ack(false)
				continue
			} else {
				slog.Warn("notification delivery failed", "retry_count", retryCount, "error", err)
			}
			retry := retryCount + 1
			if retry <= 3 {
				headers := delivery.Headers
				if headers == nil {
					headers = amqp091.Table{}
				}
				headers["x-retry-count"] = int32(retry)
				if err := p.rabbit.PublishRetry(ctx, retry, delivery.Body, headers); err == nil {
					_ = delivery.Ack(false)
					continue
				} else {
					slog.Error("notification retry publish failed", "retry_count", retry, "error", err)
				}
			}
			_ = delivery.Nack(false, false)
			slog.Error("notification dead-lettered", "retry_count", retry)
		}
	}
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
	var existing domain.Notification
	if err := p.store.DB.Collection("notifications").FindOne(ctx, bson.M{"event_id": event.EventID}).Decode(&existing); err == nil {
		return nil
	} else if err != mongo.ErrNoDocuments {
		return err
	}
	bookingID, err := primitive.ObjectIDFromHex(event.BookingID)
	if err != nil {
		return err
	}
	var booking domain.Booking
	if err := p.store.DB.Collection("bookings").FindOne(ctx, bson.M{"_id": bookingID}).Decode(&booking); err != nil {
		return err
	}
	if p.sender == nil {
		return fmt.Errorf("booking email sender is unavailable")
	}
	if err := p.sender.SendBookingConfirmation(ctx, event.EventID, booking); err != nil {
		return err
	}
	subject, message := mailer.ConfirmationContent(booking)
	now := time.Now().UTC()
	notification := domain.Notification{ID: primitive.NewObjectID(), EventID: event.EventID, BookingID: booking.ID, Recipient: booking.UserEmail, Subject: subject, Message: message, SentAt: now, CreatedAt: now}
	if _, err := p.store.DB.Collection("notifications").InsertOne(ctx, notification); err != nil && !mongo.IsDuplicateKeyError(err) {
		return err
	}
	slog.Info("booking confirmation email sent", "event_id", event.EventID, "booking_number", booking.BookingNumber, "recipient", booking.UserEmail)
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
