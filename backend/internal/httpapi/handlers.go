package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	appauth "github.com/cinema-ticket-booking/backend/internal/auth"
	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/database"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/cinema-ticket-booking/backend/internal/messaging"
	"github.com/cinema-ticket-booking/backend/internal/realtime"
	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Handlers struct {
	cfg      config.Config
	store    *database.Store
	redis    *redis.Client
	rabbit   *messaging.Rabbit
	auth     *appauth.Service
	firebase appauth.FirebaseVerifier
	google   *appauth.GoogleOAuth
	catalog  *service.CatalogService
	booking  *service.BookingService
	admin    *service.AdminService
	hub      *realtime.Hub
}

func NewHandlers(cfg config.Config, store *database.Store, redisClient *redis.Client, rabbit *messaging.Rabbit, authService *appauth.Service, firebase appauth.FirebaseVerifier, google *appauth.GoogleOAuth, catalog *service.CatalogService, booking *service.BookingService, admin *service.AdminService, hub *realtime.Hub) *Handlers {
	return &Handlers{cfg: cfg, store: store, redis: redisClient, rabbit: rabbit, auth: authService, firebase: firebase, google: google, catalog: catalog, booking: booking, admin: admin, hub: hub}
}

func (h *Handlers) FirebaseSession(c *gin.Context) {
	token := bearer(c)
	if token == "" {
		fail(c, &service.Error{Status: 401, Code: "UNAUTHENTICATED", Message: "A Firebase Bearer token is required."})
		return
	}
	principal, err := h.auth.VerifyFirebase(c.Request.Context(), token)
	if err != nil {
		h.authFailure(c, err)
		return
	}
	success(c, http.StatusOK, gin.H{"user": principal.User, "auth_method": principal.Method})
}

func (h *Handlers) Me(c *gin.Context) {
	principal := CurrentPrincipal(c)
	data := gin.H{"user": principal.User, "auth_method": principal.Method}
	if principal.Method == "google_oauth" {
		data["csrf_token"] = principal.CSRFToken
	}
	success(c, http.StatusOK, data)
}

func (h *Handlers) GoogleStart(c *gin.Context) {
	authURL, state, err := h.google.Start(c.Request.Context(), c.Query("return_to"))
	if err != nil {
		h.authFailure(c, err)
		return
	}
	h.setCookie(c, "cinema_oauth_state", state, int(h.cfg.OAuthStateTTL.Seconds()), "/api/v1/auth/google", true)
	c.Redirect(http.StatusFound, authURL)
}

func (h *Handlers) GoogleCallback(c *gin.Context) {
	stateCookie, _ := c.Cookie("cinema_oauth_state")
	claims, returnTo, err := h.google.Callback(c.Request.Context(), c.Query("state"), stateCookie, c.Query("code"))
	h.clearCookie(c, "cinema_oauth_state", "/api/v1/auth/google", true)
	if err != nil {
		c.Redirect(http.StatusFound, h.cfg.FrontendURL+"/login?error=google_oauth_failed")
		return
	}
	user, err := h.auth.ResolveIdentity(c.Request.Context(), claims)
	if err != nil {
		c.Redirect(http.StatusFound, h.cfg.FrontendURL+"/login?error=identity_link_failed")
		return
	}
	token, _, err := h.auth.CreateSession(c.Request.Context(), user.ID)
	if err != nil {
		c.Redirect(http.StatusFound, h.cfg.FrontendURL+"/login?error=session_failed")
		return
	}
	h.setCookie(c, h.auth.CookieName(), token, int(h.auth.SessionTTL().Seconds()), "/", true)
	target := h.cfg.FrontendURL + "/auth/callback"
	if returnTo != "" {
		target += "?return_to=" + url.QueryEscape(returnTo)
	}
	c.Redirect(http.StatusFound, target)
}

func (h *Handlers) Logout(c *gin.Context) {
	principal := CurrentPrincipal(c)
	if principal != nil && principal.SessionToken != "" {
		_ = h.auth.DeleteSession(c.Request.Context(), principal.SessionToken)
	}
	if token, err := c.Cookie(h.auth.CookieName()); err == nil && token != "" && (principal == nil || token != principal.SessionToken) {
		_ = h.auth.DeleteSession(c.Request.Context(), token)
	}
	h.clearCookie(c, h.auth.CookieName(), "/", true)
	success(c, http.StatusOK, gin.H{"logged_out": true})
}

func (h *Handlers) Movies(c *gin.Context) {
	movies, err := h.catalog.Movies(c.Request.Context())
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, movies)
}
func (h *Handlers) Movie(c *gin.Context) {
	id, ok := pathID(c, "movieId")
	if !ok {
		return
	}
	movie, err := h.catalog.Movie(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, movie)
}
func (h *Handlers) MovieShowtimes(c *gin.Context) {
	id, ok := pathID(c, "movieId")
	if !ok {
		return
	}
	items, err := h.catalog.MovieShowtimes(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, items)
}
func (h *Handlers) Showtime(c *gin.Context) {
	id, ok := pathID(c, "showtimeId")
	if !ok {
		return
	}
	item, err := h.catalog.Showtime(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, item)
}
func (h *Handlers) Seats(c *gin.Context) {
	id, ok := pathID(c, "showtimeId")
	if !ok {
		return
	}
	items, err := h.catalog.Seats(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, items)
}

func (h *Handlers) CreateHold(c *gin.Context) {
	showtimeID, ok := pathID(c, "showtimeId")
	if !ok {
		return
	}
	var body struct {
		SeatIDs []string `json:"seat_ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, &service.Error{Status: 422, Code: "INVALID_REQUEST", Message: "seat_ids is required."})
		return
	}
	result, err := h.booking.CreateHold(c.Request.Context(), CurrentPrincipal(c).User, showtimeID, body.SeatIDs, c.GetHeader("Idempotency-Key"))
	if err != nil {
		fail(c, err)
		return
	}
	status := http.StatusCreated
	if result.Existing {
		status = http.StatusOK
	}
	success(c, status, holdData(result.Hold))
}

func (h *Handlers) GetHold(c *gin.Context) {
	id, ok := pathID(c, "holdId")
	if !ok {
		return
	}
	hold, err := h.booking.GetHold(c.Request.Context(), CurrentPrincipal(c).User, id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, holdData(*hold))
}
func (h *Handlers) ReleaseHold(c *gin.Context) {
	id, ok := pathID(c, "holdId")
	if !ok {
		return
	}
	hold, err := h.booking.ReleaseHold(c.Request.Context(), CurrentPrincipal(c).User, id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, holdData(*hold))
}
func (h *Handlers) Confirm(c *gin.Context) {
	id, ok := pathID(c, "holdId")
	if !ok {
		return
	}
	var body struct {
		PaymentMethod string `json:"payment_method"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		fail(c, &service.Error{Status: 422, Code: "INVALID_REQUEST", Message: "A JSON payment request is required."})
		return
	}
	booking, existing, err := h.booking.Confirm(c.Request.Context(), CurrentPrincipal(c).User, id, c.GetHeader("Idempotency-Key"), body.PaymentMethod)
	if err != nil {
		fail(c, err)
		return
	}
	status := http.StatusCreated
	if existing {
		status = http.StatusOK
	}
	success(c, status, booking)
}

func (h *Handlers) MyBookings(c *gin.Context) {
	page, limit := pagination(c)
	items, total, err := h.booking.MyBookings(c.Request.Context(), CurrentPrincipal(c).User.ID, page, limit)
	if err != nil {
		fail(c, err)
		return
	}
	list(c, items, page, limit, total)
}
func (h *Handlers) Booking(c *gin.Context) {
	id, ok := pathID(c, "bookingId")
	if !ok {
		return
	}
	booking, err := h.booking.Booking(c.Request.Context(), CurrentPrincipal(c).User, id)
	if err != nil {
		fail(c, err)
		return
	}
	success(c, http.StatusOK, booking)
}

func (h *Handlers) AdminBookings(c *gin.Context) {
	page, limit := pagination(c)
	filters := service.BookingFilters{MovieID: c.Query("movie_id"), DateFrom: c.Query("date_from"), DateTo: c.Query("date_to"), UserEmail: c.Query("user_email"), BookingStatus: c.Query("booking_status")}
	items, total, err := h.admin.Bookings(c.Request.Context(), filters, page, limit)
	if err != nil {
		fail(c, err)
		return
	}
	list(c, items, page, limit, total)
}
func (h *Handlers) AdminBooking(c *gin.Context) { h.Booking(c) }
func (h *Handlers) AuditLogs(c *gin.Context) {
	page, limit := pagination(c)
	items, total, err := h.admin.AuditLogs(c.Request.Context(), service.AuditFilters{EventType: c.Query("event_type"), Severity: c.Query("severity")}, page, limit)
	if err != nil {
		fail(c, err)
		return
	}
	list(c, items, page, limit, total)
}

func (h *Handlers) WebSocket(c *gin.Context) {
	id, ok := pathID(c, "showtimeId")
	if !ok {
		return
	}
	seats, err := h.catalog.Seats(c.Request.Context(), id)
	if err != nil {
		fail(c, err)
		return
	}
	if err := h.hub.Serve(c.Writer, c.Request, id.Hex(), realtime.Snapshot(id.Hex(), seats)); err != nil {
		return
	}
}

func (h *Handlers) Live(c *gin.Context) { success(c, http.StatusOK, gin.H{"status": "live"}) }
func (h *Handlers) Ready(c *gin.Context) {
	checks := gin.H{"mongodb": false, "redis": false, "rabbitmq": false, "firebase": h.firebase != nil && h.firebase.Available(), "google_oauth": h.google != nil && h.google.Available()}
	ready := true
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if h.store.Client.Ping(ctx, nil) == nil {
		checks["mongodb"] = true
	} else {
		ready = false
	}
	if h.redis.Ping(ctx).Err() == nil {
		checks["redis"] = true
	} else {
		ready = false
	}
	if h.rabbit != nil && h.rabbit.Healthy() {
		checks["rabbitmq"] = true
	} else {
		ready = false
	}
	if !(checks["firebase"].(bool) && checks["google_oauth"].(bool)) {
		ready = false
	}
	if !ready {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"code": "NOT_READY", "message": "One or more dependencies are unavailable.", "details": checks}, "meta": gin.H{"request_id": requestID(c)}})
		return
	}
	success(c, http.StatusOK, gin.H{"status": "ready", "checks": checks})
}

func (h *Handlers) authFailure(c *gin.Context, err error) {
	if errors.Is(err, appauth.ErrProviderUnavailable) {
		fail(c, &service.Error{Status: 503, Code: "AUTH_PROVIDER_UNAVAILABLE", Message: "The authentication provider is not configured."})
		return
	}
	fail(c, &service.Error{Status: 401, Code: "UNAUTHENTICATED", Message: "Authentication failed."})
}
func (h *Handlers) setCookie(c *gin.Context, name, value string, maxAge int, path string, httpOnly bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: path, MaxAge: maxAge, HttpOnly: httpOnly, Secure: h.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}
func (h *Handlers) clearCookie(c *gin.Context, name, path string, httpOnly bool) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: "", Path: path, MaxAge: -1, Expires: time.Unix(1, 0), HttpOnly: httpOnly, Secure: h.cfg.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func pathID(c *gin.Context, name string) (primitive.ObjectID, bool) {
	id, err := primitive.ObjectIDFromHex(c.Param(name))
	if err != nil {
		fail(c, &service.Error{Status: 400, Code: "INVALID_ID", Message: fmt.Sprintf("%s is invalid.", name)})
		return primitive.NilObjectID, false
	}
	return id, true
}
func pagination(c *gin.Context) (int64, int64) {
	page, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	return page, limit
}
func holdData(hold domain.Hold) gin.H {
	remaining := int64(time.Until(hold.ExpiresAt).Seconds())
	if remaining < 0 {
		remaining = 0
	}
	seats := make([]string, len(hold.SeatIDs))
	for i, id := range hold.SeatIDs {
		seats[i] = id.Hex()
	}
	return gin.H{"hold_id": hold.ID.Hex(), "showtime_id": hold.ShowtimeID.Hex(), "seat_ids": seats, "status": hold.Status, "expires_at": hold.ExpiresAt, "remaining_seconds": remaining}
}
