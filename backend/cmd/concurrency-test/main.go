package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"time"

	appauth "github.com/cinema-ticket-booking/backend/internal/auth"
	"github.com/cinema-ticket-booking/backend/internal/bootstrap"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/cinema-ticket-booking/backend/internal/httpapi"
	seatlock "github.com/cinema-ticket-booking/backend/internal/lock"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/google/uuid"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type fakeFirebase struct{}

func (fakeFirebase) Available() bool { return true }
func (fakeFirebase) Verify(_ context.Context, token string) (appauth.Claims, error) {
	return appauth.Claims{Provider: "FIREBASE", Subject: "concurrency-" + token, FirebaseUID: "concurrency-" + token, Email: "concurrency-" + token + "@example.test", EmailVerified: true, DisplayName: "Concurrency User"}, nil
}

type apiEnvelope struct {
	Data map[string]any `json:"data"`
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	deps, err := bootstrap.Load(ctx, true)
	must(err)
	defer deps.Close(context.Background())
	testConfig := deps.Config
	testConfig.AdminEmails = make(map[string]struct{}, len(deps.Config.AdminEmails)+1)
	for email := range deps.Config.AdminEmails {
		testConfig.AdminEmails[email] = struct{}{}
	}
	testConfig.AdminEmails["concurrency-admin@example.test"] = struct{}{}
	metrics := observability.NewMetrics()
	locks := seatlock.New(deps.Redis, testConfig.SeatLockTTL)
	bookingService := service.NewBookingService(deps.Store, deps.Redis, locks, testConfig, metrics)
	catalog := service.NewCatalogService(deps.Store)
	admin := service.NewAdminService(deps.Store)
	verifier := fakeFirebase{}
	authService := appauth.NewService(deps.Store, deps.Redis, testConfig, verifier)
	handlers := httpapi.NewHandlers(testConfig, deps.Store, deps.Redis, nil, authService, verifier, nil, catalog, bookingService, admin, nil)
	server := httptest.NewServer(httpapi.Router(testConfig, handlers, authService, deps.Redis, metrics))
	defer server.Close()
	fixture := createFixture(ctx, deps)
	defer cleanup(ctx, deps, fixture)

	const attempts = 50
	var successes, conflicts, unexpected atomic.Int64
	var winner struct {
		sync.Mutex
		token, holdID string
	}
	start := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(attempts)
	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wait.Done()
			token := fmt.Sprintf("user-%02d", i)
			<-start
			status, body, err := request(server.URL+"/api/v1/showtimes/"+fixture.showtime.Hex()+"/holds", token, uuid.NewString(), map[string]any{"seat_ids": []string{fixture.seat.Hex()}})
			if err != nil {
				unexpected.Add(1)
				return
			}
			if status == http.StatusCreated {
				successes.Add(1)
				var envelope apiEnvelope
				if json.Unmarshal(body, &envelope) == nil {
					winner.Lock()
					winner.token = token
					winner.holdID, _ = envelope.Data["hold_id"].(string)
					winner.Unlock()
				}
				return
			}
			if status == http.StatusConflict {
				conflicts.Add(1)
				return
			}
			unexpected.Add(1)
		}(i)
	}
	close(start)
	wait.Wait()

	winner.Lock()
	token, holdID := winner.token, winner.holdID
	winner.Unlock()
	invariantOK := successes.Load() == 1 && conflicts.Load() == 49 && unexpected.Load() == 0 && holdID != ""
	active, _ := deps.Store.DB.Collection("holds").CountDocuments(ctx, bson.M{"showtime_id": fixture.showtime, "status": domain.HoldActive})
	invariantOK = invariantOK && active == 1
	confirmationKey := uuid.NewString()
	status1, first, err1 := request(server.URL+"/api/v1/holds/"+holdID+"/confirm", token, confirmationKey, map[string]any{"payment_method": "MOCK"})
	status2, second, err2 := request(server.URL+"/api/v1/holds/"+holdID+"/confirm", token, confirmationKey, map[string]any{"payment_method": "MOCK"})
	if err1 != nil || err2 != nil || status1 != http.StatusCreated || status2 != http.StatusOK {
		invariantOK = false
	}
	var firstEnvelope, secondEnvelope apiEnvelope
	_ = json.Unmarshal(first, &firstEnvelope)
	_ = json.Unmarshal(second, &secondEnvelope)
	invariantOK = invariantOK && firstEnvelope.Data["id"] == secondEnvelope.Data["id"]
	bookings, _ := deps.Store.DB.Collection("bookings").CountDocuments(ctx, bson.M{"showtime_id": fixture.showtime})
	var seat domain.ShowtimeSeat
	_ = deps.Store.DB.Collection("showtime_seats").FindOne(ctx, bson.M{"showtime_id": fixture.showtime, "seat_id": fixture.seat}).Decode(&seat)
	invariantOK = invariantOK && bookings == 1 && seat.Status == domain.SeatBooked
	bookingID, _ := primitive.ObjectIDFromHex(fmt.Sprint(firstEnvelope.Data["id"]))
	notificationDelivered := waitForNotification(ctx, deps, bookingID, 15*time.Second)
	invariantOK = invariantOK && notificationDelivered
	bookedRetryStatus, _, _ := request(server.URL+"/api/v1/showtimes/"+fixture.showtime.Hex()+"/holds", "booked-retry", uuid.NewString(), map[string]any{"seat_ids": []string{fixture.seat.Hex()}})
	bookedSeatProtected := bookedRetryStatus == http.StatusConflict
	invariantOK = invariantOK && bookedSeatProtected
	manualReleased := verifyManualRelease(ctx, deps, server.URL, fixture)
	invariantOK = invariantOK && manualReleased
	expiredSafely := verifyExpiration(ctx, deps, server.URL, fixture)
	invariantOK = invariantOK && expiredSafely
	authSecurity := verifyAuthSecurity(ctx, deps, server.URL, authService, testConfig.SessionCookieName)
	invariantOK = invariantOK && authSecurity
	mandatoryRouting := deps.Rabbit.Publish(ctx, "unroutable.acceptance-test", []byte(`{"event_id":"unroutable-acceptance-test"}`), nil) != nil
	invariantOK = invariantOK && mandatoryRouting

	fmt.Printf("Total attempts: %d\n", attempts)
	fmt.Printf("Successful holds: %d\n", successes.Load())
	fmt.Printf("Conflicts: %d\n", conflicts.Load())
	fmt.Printf("Unexpected errors: %d\n", unexpected.Load())
	fmt.Printf("Double booking detected: %t\n", bookings != 1)
	fmt.Printf("Booking event consumed: %t\n", notificationDelivered)
	fmt.Printf("Booked seat protected: %t\n", bookedSeatProtected)
	fmt.Printf("Manual release verified: %t\n", manualReleased)
	fmt.Printf("Expiration verified: %t\n", expiredSafely)
	fmt.Printf("Authentication linking/session/CSRF/role checks verified: %t\n", authSecurity)
	fmt.Printf("RabbitMQ mandatory routing verified: %t\n", mandatoryRouting)
	if !invariantOK {
		slog.Error("concurrency invariant failed", "active_holds", active, "bookings", bookings, "seat_status", seat.Status, "first_confirmation_status", status1, "second_confirmation_status", status2)
		os.Exit(1)
	}
}

func verifyAuthSecurity(ctx context.Context, deps *bootstrap.Dependencies, serverURL string, authService *appauth.Service, cookieName string) bool {
	email := "concurrency-linked@example.test"
	firebaseUser, err := authService.ResolveIdentity(ctx, appauth.Claims{Provider: "FIREBASE", Subject: "link-firebase", FirebaseUID: "link-firebase", Email: email, EmailVerified: true})
	if err != nil {
		return false
	}
	googleUser, err := authService.ResolveIdentity(ctx, appauth.Claims{Provider: "GOOGLE", Subject: "link-google", Email: email, EmailVerified: true})
	if err != nil || firebaseUser.ID != googleUser.ID {
		return false
	}
	identities, _ := deps.Store.DB.Collection("auth_identities").CountDocuments(ctx, bson.M{"user_id": firebaseUser.ID})
	if identities != 2 {
		return false
	}
	if _, err := authService.ResolveIdentity(ctx, appauth.Claims{Provider: "GOOGLE", Subject: "unverified", Email: "concurrency-unverified@example.test"}); !errors.Is(err, appauth.ErrUnverifiedEmail) {
		return false
	}

	adminStatus, _, _ := doRequest(http.MethodGet, serverURL+"/api/v1/admin/bookings", "admin", "", nil)
	userStatus, _, _ := doRequest(http.MethodGet, serverURL+"/api/v1/admin/bookings", "ordinary-user", "", nil)
	if adminStatus != http.StatusOK || userStatus != http.StatusForbidden {
		return false
	}

	principal, err := authService.VerifyFirebase(ctx, "cookie-session")
	if err != nil {
		return false
	}
	sessionToken, csrf, err := authService.CreateSession(ctx, principal.User.ID)
	if err != nil {
		return false
	}
	status, _, _ := credentialRequest(http.MethodPost, serverURL+"/api/v1/auth/logout", "", cookieName, sessionToken, "")
	if status != http.StatusForbidden {
		return false
	}
	status, _, _ = credentialRequest(http.MethodPost, serverURL+"/api/v1/auth/logout", "", cookieName, sessionToken, csrf)
	if status != http.StatusOK {
		return false
	}
	if _, err := authService.Session(ctx, sessionToken); !errors.Is(err, appauth.ErrUnauthenticated) {
		return false
	}

	precedenceToken, _, err := authService.CreateSession(ctx, principal.User.ID)
	if err != nil {
		return false
	}
	status, _, _ = credentialRequest(http.MethodPost, serverURL+"/api/v1/auth/logout", "ordinary-user", cookieName, precedenceToken, "")
	if status != http.StatusOK {
		return false
	}
	if _, err := authService.Session(ctx, precedenceToken); !errors.Is(err, appauth.ErrUnauthenticated) {
		return false
	}

	expiringToken, _, err := authService.CreateSession(ctx, principal.User.ID)
	if err != nil {
		return false
	}
	if err := deps.Redis.Expire(ctx, "cinema:session:"+expiringToken, time.Second).Err(); err != nil {
		return false
	}
	time.Sleep(1100 * time.Millisecond)
	_, err = authService.Session(ctx, expiringToken)
	return errors.Is(err, appauth.ErrUnauthenticated)
}

type fixtureIDs struct{ movie, auditorium, seat, expiringSeat, releaseSeat, showtime primitive.ObjectID }

func createFixture(ctx context.Context, deps *bootstrap.Dependencies) fixtureIDs {
	now := time.Now().UTC()
	fixture := fixtureIDs{movie: primitive.NewObjectID(), auditorium: primitive.NewObjectID(), seat: primitive.NewObjectID(), expiringSeat: primitive.NewObjectID(), releaseSeat: primitive.NewObjectID(), showtime: primitive.NewObjectID()}
	_, err := deps.Store.DB.Collection("movies").InsertOne(ctx, domain.Movie{ID: fixture.movie, Title: "Concurrency Fixture", DurationMinutes: 90, Status: "ACTIVE", CreatedAt: now, UpdatedAt: now})
	must(err)
	_, err = deps.Store.DB.Collection("auditoriums").InsertOne(ctx, domain.Auditorium{ID: fixture.auditorium, Name: "Concurrency Fixture", Rows: 1, SeatsPerRow: 3, CreatedAt: now, UpdatedAt: now})
	must(err)
	seatFixtures := []domain.AuditoriumSeat{{ID: fixture.seat, AuditoriumID: fixture.auditorium, Row: "T", Number: 1, Label: "T1", SeatType: "STANDARD"}, {ID: fixture.expiringSeat, AuditoriumID: fixture.auditorium, Row: "T", Number: 2, Label: "T2", SeatType: "STANDARD"}, {ID: fixture.releaseSeat, AuditoriumID: fixture.auditorium, Row: "T", Number: 3, Label: "T3", SeatType: "STANDARD"}}
	for _, seat := range seatFixtures {
		_, err = deps.Store.DB.Collection("auditorium_seats").InsertOne(ctx, seat)
		must(err)
	}
	_, err = deps.Store.DB.Collection("showtimes").InsertOne(ctx, domain.Showtime{ID: fixture.showtime, MovieID: fixture.movie, AuditoriumID: fixture.auditorium, StartTime: now.Add(24 * time.Hour), EndTime: now.Add(26 * time.Hour), Status: "ACTIVE", CreatedAt: now, UpdatedAt: now})
	must(err)
	for _, seat := range seatFixtures {
		_, err = deps.Store.DB.Collection("showtime_seats").InsertOne(ctx, domain.ShowtimeSeat{ID: primitive.NewObjectID(), ShowtimeID: fixture.showtime, SeatID: seat.ID, SeatLabel: seat.Label, Row: seat.Row, Number: seat.Number, Price: 25000, Status: domain.SeatAvailable, Version: 0, CreatedAt: now, UpdatedAt: now})
		must(err)
	}
	return fixture
}

func cleanup(ctx context.Context, deps *bootstrap.Dependencies, fixture fixtureIDs) {
	_, _ = deps.Store.DB.Collection("outbox_events").DeleteMany(ctx, bson.M{"payload.showtime_id": fixture.showtime.Hex()})
	var testBookings []domain.Booking
	if cur, err := deps.Store.DB.Collection("bookings").Find(ctx, bson.M{"showtime_id": fixture.showtime}); err == nil {
		_ = cur.All(ctx, &testBookings)
		_ = cur.Close(ctx)
	}
	bookingIDs := make([]primitive.ObjectID, len(testBookings))
	for i, booking := range testBookings {
		bookingIDs[i] = booking.ID
	}
	if len(bookingIDs) > 0 {
		_, _ = deps.Store.DB.Collection("notifications").DeleteMany(ctx, bson.M{"booking_id": bson.M{"$in": bookingIDs}})
	}
	_, _ = deps.Store.DB.Collection("bookings").DeleteMany(ctx, bson.M{"showtime_id": fixture.showtime})
	_, _ = deps.Store.DB.Collection("holds").DeleteMany(ctx, bson.M{"showtime_id": fixture.showtime})
	_, _ = deps.Store.DB.Collection("showtime_seats").DeleteMany(ctx, bson.M{"showtime_id": fixture.showtime})
	_, _ = deps.Store.DB.Collection("showtimes").DeleteOne(ctx, bson.M{"_id": fixture.showtime})
	_, _ = deps.Store.DB.Collection("auditorium_seats").DeleteMany(ctx, bson.M{"auditorium_id": fixture.auditorium})
	_, _ = deps.Store.DB.Collection("auditoriums").DeleteOne(ctx, bson.M{"_id": fixture.auditorium})
	_, _ = deps.Store.DB.Collection("movies").DeleteOne(ctx, bson.M{"_id": fixture.movie})
	cur, _ := deps.Store.DB.Collection("users").Find(ctx, bson.M{"email": bson.M{"$regex": "^concurrency-"}}, options.Find().SetProjection(bson.M{"_id": 1}))
	var users []domain.User
	if cur != nil {
		_ = cur.All(ctx, &users)
		_ = cur.Close(ctx)
	}
	ids := make([]primitive.ObjectID, len(users))
	for i, user := range users {
		ids[i] = user.ID
	}
	if len(ids) > 0 {
		_, _ = deps.Store.DB.Collection("audit_logs").DeleteMany(ctx, bson.M{"actor_user_id": bson.M{"$in": ids}})
		_, _ = deps.Store.DB.Collection("auth_identities").DeleteMany(ctx, bson.M{"user_id": bson.M{"$in": ids}})
		_, _ = deps.Store.DB.Collection("users").DeleteMany(ctx, bson.M{"_id": bson.M{"$in": ids}})
	}
	_ = deps.Redis.Del(ctx, seatlock.SeatKey(fixture.showtime.Hex(), fixture.seat.Hex()), seatlock.SeatKey(fixture.showtime.Hex(), fixture.expiringSeat.Hex()), seatlock.SeatKey(fixture.showtime.Hex(), fixture.releaseSeat.Hex())).Err()
}

func verifyManualRelease(ctx context.Context, deps *bootstrap.Dependencies, serverURL string, fixture fixtureIDs) bool {
	status, raw, err := request(serverURL+"/api/v1/showtimes/"+fixture.showtime.Hex()+"/holds", "manual-release", uuid.NewString(), map[string]any{"seat_ids": []string{fixture.releaseSeat.Hex()}})
	if err != nil || status != http.StatusCreated {
		return false
	}
	var envelope apiEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	holdID := fmt.Sprint(envelope.Data["hold_id"])
	status, _, err = doRequest(http.MethodDelete, serverURL+"/api/v1/holds/"+holdID, "manual-release", "", nil)
	if err != nil || status != http.StatusOK {
		return false
	}
	id, _ := primitive.ObjectIDFromHex(holdID)
	var hold domain.Hold
	var seat domain.ShowtimeSeat
	_ = deps.Store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": id}).Decode(&hold)
	_ = deps.Store.DB.Collection("showtime_seats").FindOne(ctx, bson.M{"showtime_id": fixture.showtime, "seat_id": fixture.releaseSeat}).Decode(&seat)
	audits, _ := deps.Store.DB.Collection("audit_logs").CountDocuments(ctx, bson.M{"entity_id": holdID, "event_type": "SEAT_RELEASED"})
	return hold.Status == domain.HoldReleased && seat.Status == domain.SeatAvailable && audits == 1
}

func verifyExpiration(ctx context.Context, deps *bootstrap.Dependencies, serverURL string, fixture fixtureIDs) bool {
	status, raw, err := request(serverURL+"/api/v1/showtimes/"+fixture.showtime.Hex()+"/holds", "expiration", uuid.NewString(), map[string]any{"seat_ids": []string{fixture.expiringSeat.Hex()}})
	if err != nil || status != http.StatusCreated {
		return false
	}
	var envelope apiEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return false
	}
	holdID := fmt.Sprint(envelope.Data["hold_id"])
	id, _ := primitive.ObjectIDFromHex(holdID)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		var hold domain.Hold
		var seat domain.ShowtimeSeat
		_ = deps.Store.DB.Collection("holds").FindOne(ctx, bson.M{"_id": id}).Decode(&hold)
		_ = deps.Store.DB.Collection("showtime_seats").FindOne(ctx, bson.M{"showtime_id": fixture.showtime, "seat_id": fixture.expiringSeat}).Decode(&seat)
		if hold.Status == domain.HoldExpired && seat.Status == domain.SeatAvailable {
			timeoutAudits, _ := deps.Store.DB.Collection("audit_logs").CountDocuments(ctx, bson.M{"entity_id": holdID, "event_type": "BOOKING_TIMEOUT"})
			releaseAudits, _ := deps.Store.DB.Collection("audit_logs").CountDocuments(ctx, bson.M{"entity_id": holdID, "event_type": "SEAT_RELEASED"})
			return timeoutAudits == 1 && releaseAudits == 1
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func waitForNotification(ctx context.Context, deps *bootstrap.Dependencies, bookingID primitive.ObjectID, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		count, err := deps.Store.DB.Collection("notifications").CountDocuments(ctx, bson.M{"booking_id": bookingID})
		if err == nil && count == 1 {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(200 * time.Millisecond):
		}
	}
	return false
}

func request(baseURL, token, key string, body any) (int, []byte, error) {
	return doRequest(http.MethodPost, baseURL, token, key, body)
}

func doRequest(method, baseURL, token, key string, body any) (int, []byte, error) {
	raw, _ := json.Marshal(body)
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, baseURL, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	return response.StatusCode, payload, err
}

func credentialRequest(method, requestURL, bearerToken, cookieName, cookieValue, csrf string) (int, []byte, error) {
	req, _ := http.NewRequest(method, requestURL, nil)
	if bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	if cookieValue != "" {
		req.AddCookie(&http.Cookie{Name: cookieName, Value: cookieValue})
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	return response.StatusCode, payload, err
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
