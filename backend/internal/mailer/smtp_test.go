package mailer

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/domain"
)

func TestConfirmationContent(t *testing.T) {
	booking := domain.Booking{
		BookingNumber:  "BKG-20260715-ABC12345",
		MovieTitle:     "The Last Orbit",
		ShowtimeStart:  time.Date(2026, 7, 16, 18, 30, 0, 0, time.FixedZone("ICT", 7*60*60)),
		AuditoriumName: "Auditorium 1",
		Seats:          []domain.BookingSeat{{Label: "A2"}, {Label: "A1"}},
		TotalAmount:    50000,
		Currency:       "THB",
	}
	subject, body := ConfirmationContent(booking)
	if subject != "Booking BKG-20260715-ABC12345 confirmed" {
		t.Fatalf("unexpected subject: %q", subject)
	}
	for _, expected := range []string{
		"Booking number: BKG-20260715-ABC12345",
		"Movie: The Last Orbit",
		"Showtime: 2026-07-16T11:30:00Z",
		"Auditorium: Auditorium 1",
		"Seats: A1, A2",
		"Total: THB 500.00",
	} {
		if !strings.Contains(body, expected) {
			t.Fatalf("body does not contain %q:\n%s", expected, body)
		}
	}
}

func TestSendBookingConfirmationRejectsInvalidRecipientBeforeDial(t *testing.T) {
	sender := &SMTP{host: "invalid.invalid", port: 1025, from: "no-reply@cinema.local"}
	err := sender.SendBookingConfirmation(context.Background(), "event-1", domain.Booking{UserEmail: "not-an-email"})
	if err == nil || !strings.Contains(err.Error(), "parse booking recipient") {
		t.Fatalf("expected recipient validation error, got %v", err)
	}
}

func TestConfirmationMessageIDIsStableAndValidated(t *testing.T) {
	first, err := confirmationMessageID("event-123")
	if err != nil {
		t.Fatal(err)
	}
	second, err := confirmationMessageID("event-123")
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != "<event-123@cinema-ticket-booking.local>" {
		t.Fatalf("unexpected message ID: %q", first)
	}
	if _, err := confirmationMessageID("bad\r\nevent"); err == nil {
		t.Fatal("expected unsafe event ID to fail")
	}
}
