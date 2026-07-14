package httpapi

import (
	"net/http"
	"time"

	appauth "github.com/cinema-ticket-booking/backend/internal/auth"
	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/observability"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

func Router(cfg config.Config, handlers *Handlers, authService *appauth.Service, redisClient *redis.Client, metrics *observability.Metrics) *gin.Engine {
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(RequestID(), gin.Recovery(), SecurityHeaders(cfg), cors.New(cors.Config{AllowOrigins: cfg.AllowedOrigins, AllowMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, AllowHeaders: []string{"Authorization", "Content-Type", "Idempotency-Key", "X-CSRF-Token", "X-Request-ID"}, ExposeHeaders: []string{"X-Request-ID"}, AllowCredentials: true, MaxAge: 12 * time.Hour}), otelgin.Middleware("cinema-api"), metrics.Middleware(), RequestLogger())
	r.GET("/health/live", handlers.Live)
	r.GET("/health/ready", handlers.Ready)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.StaticFile("/openapi.yaml", "./docs/openapi.yaml")
	v1 := r.Group("/api/v1")
	{
		authRoutes := v1.Group("/auth")
		authRoutes.POST("/session", handlers.FirebaseSession)
		authRoutes.GET("/google/start", handlers.GoogleStart)
		authRoutes.GET("/google/callback", handlers.GoogleCallback)
		authenticatedAuth := authRoutes.Group("")
		authenticatedAuth.Use(RequireAuth(authService, metrics))
		authenticatedAuth.GET("/me", handlers.Me)
		authenticatedAuth.POST("/logout", handlers.Logout)
		v1.GET("/movies", handlers.Movies)
		v1.GET("/movies/:movieId", handlers.Movie)
		v1.GET("/movies/:movieId/showtimes", handlers.MovieShowtimes)
		v1.GET("/showtimes/:showtimeId", handlers.Showtime)
		v1.GET("/showtimes/:showtimeId/seats", handlers.Seats)
		v1.GET("/ws/showtimes/:showtimeId", handlers.WebSocket)
		protected := v1.Group("")
		protected.Use(RequireAuth(authService, metrics))
		protected.POST("/showtimes/:showtimeId/holds", HoldRateLimit(redisClient, cfg), handlers.CreateHold)
		protected.GET("/holds/:holdId", handlers.GetHold)
		protected.DELETE("/holds/:holdId", handlers.ReleaseHold)
		protected.POST("/holds/:holdId/confirm", handlers.Confirm)
		protected.GET("/bookings/me", handlers.MyBookings)
		protected.GET("/bookings/:bookingId", handlers.Booking)
		admin := v1.Group("/admin")
		admin.Use(RequireAuth(authService, metrics), RequireAdmin())
		admin.GET("/bookings", handlers.AdminBookings)
		admin.GET("/bookings/:bookingId", handlers.AdminBooking)
		admin.GET("/audit-logs", handlers.AuditLogs)
	}
	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"code": "NOT_FOUND", "message": "Route not found.", "details": gin.H{}}, "meta": gin.H{"request_id": requestID(c)}})
	})
	return r
}
