package service

import (
	"context"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type CatalogService struct{ store *database.Store }

func NewCatalogService(store *database.Store) *CatalogService { return &CatalogService{store: store} }

func (s *CatalogService) Movies(ctx context.Context) ([]domain.Movie, error) {
	cur, err := s.store.DB.Collection("movies").Find(ctx, bson.M{"status": "ACTIVE"}, options.Find().SetSort(bson.D{{Key: "title", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []domain.Movie
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *CatalogService) Movie(ctx context.Context, id primitive.ObjectID) (*domain.Movie, error) {
	var out domain.Movie
	err := s.store.DB.Collection("movies").FindOne(ctx, bson.M{"_id": id, "status": "ACTIVE"}).Decode(&out)
	if err == mongo.ErrNoDocuments {
		return nil, problem(404, "MOVIE_NOT_FOUND", "Movie not found.", nil)
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *CatalogService) MovieShowtimes(ctx context.Context, movieID primitive.ObjectID) ([]domain.Showtime, error) {
	cur, err := s.store.DB.Collection("showtimes").Find(ctx, bson.M{"movie_id": movieID, "status": "ACTIVE", "start_time": bson.M{"$gt": time.Now().UTC()}}, options.Find().SetSort(bson.D{{Key: "start_time", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []domain.Showtime
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

type ShowtimeDetail struct {
	Showtime   domain.Showtime   `json:"showtime"`
	Movie      domain.Movie      `json:"movie"`
	Auditorium domain.Auditorium `json:"auditorium"`
}

func (s *CatalogService) Showtime(ctx context.Context, id primitive.ObjectID) (*ShowtimeDetail, error) {
	var show domain.Showtime
	if err := s.store.DB.Collection("showtimes").FindOne(ctx, bson.M{"_id": id}).Decode(&show); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, problem(404, "SHOWTIME_NOT_FOUND", "Showtime not found.", nil)
		}
		return nil, err
	}
	var movie domain.Movie
	if err := s.store.DB.Collection("movies").FindOne(ctx, bson.M{"_id": show.MovieID}).Decode(&movie); err != nil {
		return nil, err
	}
	var auditorium domain.Auditorium
	if err := s.store.DB.Collection("auditoriums").FindOne(ctx, bson.M{"_id": show.AuditoriumID}).Decode(&auditorium); err != nil {
		return nil, err
	}
	return &ShowtimeDetail{Showtime: show, Movie: movie, Auditorium: auditorium}, nil
}

func (s *CatalogService) Seats(ctx context.Context, showtimeID primitive.ObjectID) ([]domain.ShowtimeSeat, error) {
	cur, err := s.store.DB.Collection("showtime_seats").Find(ctx, bson.M{"showtime_id": showtimeID}, options.Find().SetSort(bson.D{{Key: "row", Value: 1}, {Key: "number", Value: 1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []domain.ShowtimeSeat
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		var show domain.Showtime
		if err := s.store.DB.Collection("showtimes").FindOne(ctx, bson.M{"_id": showtimeID}).Decode(&show); err == mongo.ErrNoDocuments {
			return nil, problem(404, "SHOWTIME_NOT_FOUND", "Showtime not found.", nil)
		}
	}
	return out, nil
}
