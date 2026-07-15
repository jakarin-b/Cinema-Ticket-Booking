package mailer

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/domain"
)

const sendTimeout = 10 * time.Second

type Sender interface {
	SendBookingConfirmation(context.Context, string, domain.Booking) error
}

type SMTP struct {
	host     string
	port     int
	username string
	password string
	from     string
}

func NewSMTP(cfg config.Config) *SMTP {
	return &SMTP{host: cfg.SMTPHost, port: cfg.SMTPPort, username: cfg.SMTPUsername, password: cfg.SMTPPassword, from: cfg.SMTPFrom}
}

func ConfirmationContent(booking domain.Booking) (string, string) {
	labels := make([]string, len(booking.Seats))
	for i, seat := range booking.Seats {
		labels[i] = seat.Label
	}
	sort.Strings(labels)
	subject := cleanHeader("Booking " + booking.BookingNumber + " confirmed")
	body := fmt.Sprintf(`Hello,

Your booking is confirmed.

Booking number: %s
Movie: %s
Showtime: %s
Auditorium: %s
Seats: %s
Total: %s

Thank you for booking with Cinema Ticket Booking.
`, booking.BookingNumber, booking.MovieTitle, booking.ShowtimeStart.UTC().Format(time.RFC3339), booking.AuditoriumName, strings.Join(labels, ", "), formatAmount(booking.Currency, booking.TotalAmount))
	return subject, body
}

func (s *SMTP) SendBookingConfirmation(ctx context.Context, eventID string, booking domain.Booking) error {
	from, err := mail.ParseAddress(s.from)
	if err != nil {
		return fmt.Errorf("parse SMTP sender: %w", err)
	}
	to, err := mail.ParseAddress(booking.UserEmail)
	if err != nil {
		return fmt.Errorf("parse booking recipient: %w", err)
	}
	messageID, err := confirmationMessageID(eventID)
	if err != nil {
		return err
	}
	subject, body := ConfirmationContent(booking)
	raw := strings.Join([]string{
		"From: " + from.String(),
		"To: " + to.String(),
		"Subject: " + subject,
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"Message-ID: " + messageID,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
		"",
		strings.ReplaceAll(body, "\n", "\r\n"),
	}, "\r\n")

	sendCtx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	address := net.JoinHostPort(s.host, fmt.Sprintf("%d", s.port))
	conn, err := (&net.Dialer{}).DialContext(sendCtx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect SMTP server: %w", err)
	}
	if deadline, ok := sendCtx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("create SMTP client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("start SMTP TLS: %w", err)
		}
	}
	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("authenticate SMTP client: %w", err)
		}
	}
	if err := client.Mail(from.Address); err != nil {
		return fmt.Errorf("set SMTP sender: %w", err)
	}
	if err := client.Rcpt(to.Address); err != nil {
		return fmt.Errorf("set SMTP recipient: %w", err)
	}
	writer, err := client.Data()
	if err != nil {
		return fmt.Errorf("open SMTP message body: %w", err)
	}
	if _, err := writer.Write([]byte(raw)); err != nil {
		_ = writer.Close()
		return fmt.Errorf("write SMTP message: %w", err)
	}
	if err := writer.Close(); err != nil {
		return fmt.Errorf("commit SMTP message: %w", err)
	}
	if err := client.Quit(); err != nil {
		return fmt.Errorf("close SMTP session: %w", err)
	}
	return nil
}

func confirmationMessageID(eventID string) (string, error) {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return "", fmt.Errorf("event_id is required")
	}
	for _, r := range eventID {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_' && r != '.' {
			return "", fmt.Errorf("event_id contains invalid message ID characters")
		}
	}
	return "<" + eventID + "@cinema-ticket-booking.local>", nil
}

func cleanHeader(value string) string {
	return strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ").Replace(value))
}

func formatAmount(currency string, amount int64) string {
	currency = cleanHeader(currency)
	return fmt.Sprintf("%s %d.%02d", currency, amount/100, amount%100)
}
