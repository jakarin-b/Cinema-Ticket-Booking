package seed

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func Run(ctx context.Context, store *database.Store) error {
	now := time.Now().UTC()
	movie1, movie2, auditoriumID := id(1), id(2), id(10)
	movies := []domain.Movie{{ID: movie1, Title: "The Last Orbit", Description: "A stranded crew races to bring a damaged research vessel home.", DurationMinutes: 128, PosterURL: "https://images.unsplash.com/photo-1440404653325-ab127d49abc1?auto=format&fit=crop&w=600&q=80", Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}, {ID: movie2, Title: "Bangkok After Rain", Description: "Two strangers cross paths during one unforgettable night in Bangkok.", DurationMinutes: 112, PosterURL: "https://images.unsplash.com/photo-1500530855697-b586d89ba3ee?auto=format&fit=crop&w=600&q=80", Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}}
	for _, movie := range movies {
		if _, err := store.DB.Collection("movies").ReplaceOne(ctx, bson.M{"_id": movie.ID}, movie, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
	}
	auditorium := domain.Auditorium{ID: auditoriumID, Name: "Auditorium 1", Rows: 8, SeatsPerRow: 10, CreatedAt: now, UpdatedAt: now}
	if _, err := store.DB.Collection("auditoriums").ReplaceOne(ctx, bson.M{"_id": auditorium.ID}, auditorium, options.Replace().SetUpsert(true)); err != nil {
		return err
	}
	var seats []domain.AuditoriumSeat
	for row := 0; row < 8; row++ {
		for number := 1; number <= 10; number++ {
			seatID := id(1000 + row*10 + number)
			rowName := string(rune('A' + row))
			seat := domain.AuditoriumSeat{ID: seatID, AuditoriumID: auditoriumID, Row: rowName, Number: number, Label: fmt.Sprintf("%s%d", rowName, number), SeatType: "STANDARD"}
			seats = append(seats, seat)
			if _, err := store.DB.Collection("auditorium_seats").ReplaceOne(ctx, bson.M{"_id": seat.ID}, seat, options.Replace().SetUpsert(true)); err != nil {
				return err
			}
		}
	}
	startBase := now.Truncate(24 * time.Hour).Add(24*time.Hour + 11*time.Hour)
	showtimes := []domain.Showtime{{ID: id(100), MovieID: movie1, AuditoriumID: auditoriumID, StartTime: startBase, EndTime: startBase.Add(128 * time.Minute), Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}, {ID: id(101), MovieID: movie2, AuditoriumID: auditoriumID, StartTime: startBase.Add(24 * time.Hour), EndTime: startBase.Add(24*time.Hour + 112*time.Minute), Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}, {ID: id(102), MovieID: movie1, AuditoriumID: auditoriumID, StartTime: startBase.Add(48 * time.Hour), EndTime: startBase.Add(48*time.Hour + 128*time.Minute), Status: "ACTIVE", CreatedAt: now, UpdatedAt: now}}
	for _, showtime := range showtimes {
		if _, err := store.DB.Collection("showtimes").ReplaceOne(ctx, bson.M{"_id": showtime.ID}, showtime, options.Replace().SetUpsert(true)); err != nil {
			return err
		}
		for _, seat := range seats {
			docID := materializedID(showtime.ID, seat.ID)
			filter := bson.M{"showtime_id": showtime.ID, "seat_id": seat.ID}
			static := bson.M{"_id": docID, "showtime_id": showtime.ID, "seat_id": seat.ID, "status": domain.SeatAvailable, "version": int64(0), "created_at": now}
			update := bson.M{"$setOnInsert": static, "$set": bson.M{"seat_label": seat.Label, "row": seat.Row, "number": seat.Number, "price": int64(25000), "updated_at": now}}
			if _, err := store.DB.Collection("showtime_seats").UpdateOne(ctx, filter, update, options.Update().SetUpsert(true)); err != nil && !mongo.IsDuplicateKeyError(err) {
				return err
			}
		}
	}
	return nil
}

func id(number int) primitive.ObjectID {
	value := fmt.Sprintf("6500000000000000%08x", number)
	result, _ := primitive.ObjectIDFromHex(value)
	return result
}
func materializedID(showtimeID, seatID primitive.ObjectID) primitive.ObjectID {
	sum := sha256.Sum256([]byte(showtimeID.Hex() + ":" + seatID.Hex()))
	var result primitive.ObjectID
	copy(result[:], sum[:12])
	return result
}
