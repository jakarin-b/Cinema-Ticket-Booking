package messaging

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/rabbitmq/amqp091-go"
)

const deadExchange = "cinema.dlx"

type Rabbit struct {
	conn      *amqp091.Connection
	publisher *amqp091.Channel
	returns   <-chan amqp091.Return
	cfg       config.Config
	mu        sync.Mutex
}

func Connect(cfg config.Config) (*Rabbit, error) {
	conn, err := amqp091.Dial(cfg.RabbitURL)
	if err != nil {
		return nil, err
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	returnCh := make(chan amqp091.Return, 1)
	r := &Rabbit{conn: conn, publisher: ch, returns: ch.NotifyReturn(returnCh), cfg: cfg}
	if err := r.declare(ch); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return nil, err
	}
	if err := ch.Confirm(false); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rabbit) declare(ch *amqp091.Channel) error {
	if err := ch.ExchangeDeclare(r.cfg.RabbitExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.ExchangeDeclare(deadExchange, "direct", true, false, false, false, nil); err != nil {
		return err
	}
	args := amqp091.Table{"x-dead-letter-exchange": deadExchange, "x-dead-letter-routing-key": "booking.notification.dead"}
	if _, err := ch.QueueDeclare(r.cfg.RabbitQueue, true, false, false, false, args); err != nil {
		return err
	}
	if err := ch.QueueBind(r.cfg.RabbitQueue, "booking.confirmed", r.cfg.RabbitExchange, false, nil); err != nil {
		return err
	}
	dlq := r.cfg.RabbitQueue + ".dlq"
	if _, err := ch.QueueDeclare(dlq, true, false, false, false, nil); err != nil {
		return err
	}
	if err := ch.QueueBind(dlq, "booking.notification.dead", deadExchange, false, nil); err != nil {
		return err
	}
	for i, delay := range []int32{5000, 30000, 120000} {
		name := fmt.Sprintf("%s.retry.%d", r.cfg.RabbitQueue, i+1)
		retryArgs := amqp091.Table{"x-message-ttl": delay, "x-dead-letter-exchange": r.cfg.RabbitExchange, "x-dead-letter-routing-key": "booking.confirmed"}
		if _, err := ch.QueueDeclare(name, true, false, false, false, retryArgs); err != nil {
			return err
		}
	}
	return nil
}

func (r *Rabbit) Close() error {
	if r.publisher != nil {
		_ = r.publisher.Close()
	}
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}
func (r *Rabbit) Healthy() bool { return r != nil && r.conn != nil && !r.conn.IsClosed() }

func (r *Rabbit) Publish(ctx context.Context, routingKey string, payload []byte, headers amqp091.Table) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.Healthy() {
		return errors.New("RabbitMQ connection is closed")
	}
	confirmation, err := r.publisher.PublishWithDeferredConfirmWithContext(ctx, r.cfg.RabbitExchange, routingKey, true, false, amqp091.Publishing{DeliveryMode: amqp091.Persistent, ContentType: "application/json", MessageId: messageID(payload), Timestamp: time.Now().UTC(), Headers: headers, Body: payload})
	if err != nil {
		return err
	}
	if confirmation == nil {
		return errors.New("publisher confirmation unavailable")
	}
	return r.waitForPublish(ctx, confirmation)
}

func (r *Rabbit) PublishRetry(ctx context.Context, retry int, payload []byte, headers amqp091.Table) error {
	if retry < 1 || retry > 3 {
		return errors.New("invalid retry number")
	}
	name := fmt.Sprintf("%s.retry.%d", r.cfg.RabbitQueue, retry)
	r.mu.Lock()
	defer r.mu.Unlock()
	confirmation, err := r.publisher.PublishWithDeferredConfirmWithContext(ctx, "", name, true, false, amqp091.Publishing{DeliveryMode: amqp091.Persistent, ContentType: "application/json", Timestamp: time.Now().UTC(), Headers: headers, Body: payload})
	if err != nil {
		return err
	}
	return r.waitForPublish(ctx, confirmation)
}

func (r *Rabbit) waitForPublish(ctx context.Context, confirmation *amqp091.DeferredConfirmation) error {
	ok, err := confirmation.WaitContext(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("RabbitMQ negatively acknowledged publish")
	}
	select {
	case returned := <-r.returns:
		return fmt.Errorf("RabbitMQ returned unroutable message: %d %s", returned.ReplyCode, returned.ReplyText)
	default:
		return nil
	}
}

func (r *Rabbit) Consume() (<-chan amqp091.Delivery, *amqp091.Channel, error) {
	ch, err := r.conn.Channel()
	if err != nil {
		return nil, nil, err
	}
	if err := r.declare(ch); err != nil {
		_ = ch.Close()
		return nil, nil, err
	}
	if err := ch.Qos(10, 0, false); err != nil {
		_ = ch.Close()
		return nil, nil, err
	}
	deliveries, err := ch.Consume(r.cfg.RabbitQueue, "", false, false, false, false, nil)
	if err != nil {
		_ = ch.Close()
		return nil, nil, err
	}
	return deliveries, ch, nil
}

func messageID(payload []byte) string {
	var event struct {
		EventID string `json:"event_id"`
	}
	_ = json.Unmarshal(payload, &event)
	return event.EventID
}
