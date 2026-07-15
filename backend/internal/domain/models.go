package domain

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const (
	RoleUser      = "USER"
	RoleAdmin     = "ADMIN"
	SeatAvailable = "AVAILABLE"
	SeatLocked    = "LOCKED"
	SeatBooked    = "BOOKED"
	HoldActive    = "ACTIVE"
	HoldConfirmed = "CONFIRMED"
	HoldExpired   = "EXPIRED"
	HoldReleased  = "RELEASED"
)

type User struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	FirebaseUID string             `bson:"firebase_uid,omitempty" json:"-"`
	Email       string             `bson:"email" json:"email"`
	DisplayName string             `bson:"display_name" json:"display_name"`
	AvatarURL   string             `bson:"avatar_url,omitempty" json:"avatar_url,omitempty"`
	Role        string             `bson:"role" json:"role"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}

type AuthIdentity struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Provider      string             `bson:"provider" json:"provider"`
	Subject       string             `bson:"subject" json:"-"`
	UserID        primitive.ObjectID `bson:"user_id" json:"user_id"`
	VerifiedEmail string             `bson:"verified_email" json:"verified_email"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	LastLoginAt   time.Time          `bson:"last_login_at" json:"last_login_at"`
}

type Movie struct {
	ID              primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Title           string             `bson:"title" json:"title"`
	Description     string             `bson:"description" json:"description"`
	DurationMinutes int                `bson:"duration_minutes" json:"duration_minutes"`
	PosterURL       string             `bson:"poster_url" json:"poster_url"`
	Status          string             `bson:"status" json:"status"`
	CreatedAt       time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt       time.Time          `bson:"updated_at" json:"updated_at"`
}

type Auditorium struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Rows        int                `bson:"rows" json:"rows"`
	SeatsPerRow int                `bson:"seats_per_row" json:"seats_per_row"`
	CreatedAt   time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt   time.Time          `bson:"updated_at" json:"updated_at"`
}

type AuditoriumSeat struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	AuditoriumID primitive.ObjectID `bson:"auditorium_id" json:"auditorium_id"`
	Row          string             `bson:"row" json:"row"`
	Number       int                `bson:"number" json:"number"`
	Label        string             `bson:"label" json:"label"`
	SeatType     string             `bson:"seat_type" json:"seat_type"`
}

type Showtime struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	MovieID      primitive.ObjectID `bson:"movie_id" json:"movie_id"`
	AuditoriumID primitive.ObjectID `bson:"auditorium_id" json:"auditorium_id"`
	StartTime    time.Time          `bson:"start_time" json:"start_time"`
	EndTime      time.Time          `bson:"end_time" json:"end_time"`
	Status       string             `bson:"status" json:"status"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}

type ShowtimeSeat struct {
	ID             primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	ShowtimeID     primitive.ObjectID  `bson:"showtime_id" json:"showtime_id"`
	SeatID         primitive.ObjectID  `bson:"seat_id" json:"seat_id"`
	SeatLabel      string              `bson:"seat_label" json:"seat_label"`
	Row            string              `bson:"row" json:"row"`
	Number         int                 `bson:"number" json:"number"`
	Price          int64               `bson:"price" json:"price"`
	Status         string              `bson:"status" json:"status"`
	HoldID         *primitive.ObjectID `bson:"hold_id,omitempty" json:"hold_id,omitempty"`
	LockedByUserID *primitive.ObjectID `bson:"locked_by_user_id,omitempty" json:"-"`
	LockExpiresAt  *time.Time          `bson:"lock_expires_at,omitempty" json:"lock_expires_at,omitempty"`
	BookingID      *primitive.ObjectID `bson:"booking_id,omitempty" json:"booking_id,omitempty"`
	Version        int64               `bson:"version" json:"version"`
	CreatedAt      time.Time           `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time           `bson:"updated_at" json:"updated_at"`
}

type Hold struct {
	ID             primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	UserID         primitive.ObjectID   `bson:"user_id" json:"user_id"`
	ShowtimeID     primitive.ObjectID   `bson:"showtime_id" json:"showtime_id"`
	SeatIDs        []primitive.ObjectID `bson:"seat_ids" json:"seat_ids"`
	LockToken      string               `bson:"lock_token" json:"-"`
	Status         string               `bson:"status" json:"status"`
	ExpiresAt      time.Time            `bson:"expires_at" json:"expires_at"`
	IdempotencyKey string               `bson:"idempotency_key" json:"-"`
	CreatedAt      time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt      time.Time            `bson:"updated_at" json:"updated_at"`
}

type BookingSeat struct {
	SeatID primitive.ObjectID `bson:"seat_id" json:"seat_id"`
	Label  string             `bson:"label" json:"label"`
	Price  int64              `bson:"price" json:"price"`
}

type Booking struct {
	ID                         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	BookingNumber              string             `bson:"booking_number" json:"booking_number"`
	HoldID                     primitive.ObjectID `bson:"hold_id" json:"hold_id"`
	UserID                     primitive.ObjectID `bson:"user_id" json:"user_id"`
	UserEmail                  string             `bson:"user_email" json:"user_email"`
	ShowtimeID                 primitive.ObjectID `bson:"showtime_id" json:"showtime_id"`
	MovieID                    primitive.ObjectID `bson:"movie_id" json:"movie_id"`
	MovieTitle                 string             `bson:"movie_title" json:"movie_title"`
	ShowtimeStart              time.Time          `bson:"showtime_start" json:"showtime_start"`
	AuditoriumName             string             `bson:"auditorium_name" json:"auditorium_name"`
	Seats                      []BookingSeat      `bson:"seats" json:"seats"`
	TotalAmount                int64              `bson:"total_amount" json:"total_amount"`
	Currency                   string             `bson:"currency" json:"currency"`
	PaymentStatus              string             `bson:"payment_status" json:"payment_status"`
	BookingStatus              string             `bson:"booking_status" json:"booking_status"`
	ConfirmationIdempotencyKey string             `bson:"confirmation_idempotency_key" json:"-"`
	CreatedAt                  time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt                  time.Time          `bson:"updated_at" json:"updated_at"`
}

type AuditLog struct {
	ID          primitive.ObjectID  `bson:"_id,omitempty" json:"id"`
	EventType   string              `bson:"event_type" json:"event_type"`
	ActorUserID *primitive.ObjectID `bson:"actor_user_id,omitempty" json:"actor_user_id,omitempty"`
	EntityType  string              `bson:"entity_type" json:"entity_type"`
	EntityID    string              `bson:"entity_id" json:"entity_id"`
	Metadata    map[string]any      `bson:"metadata,omitempty" json:"metadata,omitempty"`
	Severity    string              `bson:"severity" json:"severity"`
	CreatedAt   time.Time           `bson:"created_at" json:"created_at"`
}

type OutboxEvent struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	EventID       string             `bson:"event_id" json:"event_id"`
	EventType     string             `bson:"event_type" json:"event_type"`
	Payload       map[string]any     `bson:"payload" json:"payload"`
	Status        string             `bson:"status" json:"status"`
	Attempts      int                `bson:"attempts" json:"attempts"`
	NextAttemptAt time.Time          `bson:"next_attempt_at" json:"next_attempt_at"`
	LeaseUntil    *time.Time         `bson:"lease_until,omitempty" json:"lease_until,omitempty"`
	LastError     string             `bson:"last_error,omitempty" json:"last_error,omitempty"`
	CreatedAt     time.Time          `bson:"created_at" json:"created_at"`
	PublishedAt   *time.Time         `bson:"published_at,omitempty" json:"published_at,omitempty"`
}

type Notification struct {
	ID        primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	EventID   string             `bson:"event_id" json:"event_id"`
	BookingID primitive.ObjectID `bson:"booking_id" json:"booking_id"`
	Recipient string             `bson:"recipient,omitempty" json:"recipient,omitempty"`
	Subject   string             `bson:"subject,omitempty" json:"subject,omitempty"`
	Message   string             `bson:"message" json:"message"`
	SentAt    time.Time          `bson:"sent_at,omitempty" json:"sent_at,omitempty"`
	CreatedAt time.Time          `bson:"created_at" json:"created_at"`
}

type SeatEvent struct {
	EventID    string         `json:"event_id"`
	Type       string         `json:"type"`
	ShowtimeID string         `json:"showtime_id"`
	OccurredAt time.Time      `json:"occurred_at"`
	Data       map[string]any `json:"data"`
}
