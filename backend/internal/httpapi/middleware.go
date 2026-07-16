package httpapi

import (
	"context"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	appauth "github.com/cinema-ticket-booking/backend/internal/auth"
	"github.com/cinema-ticket-booking/backend/internal/config"
	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/cinema-ticket-booking/backend/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const principalKey = "principal"

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-ID")
		if _, err := uuid.Parse(id); err != nil {
			id = uuid.NewString()
		}
		c.Set("request_id", id)
		c.Header("X-Request-ID", id)
		c.Next()
	}
}
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		slog.Info("http request", "request_id", requestID(c), "method", c.Request.Method, "path", c.Request.URL.Path, "route", c.FullPath(), "status", c.Writer.Status(), "latency_ms", time.Since(start).Milliseconds(), "client_ip", c.ClientIP())
	}
}
func SecurityHeaders(cfg config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if cfg.AppEnv == "production" {
			c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
		}
		c.Next()
	}
}

func RequireAuth(authService *appauth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, err := authenticate(c, authService)
		if err != nil {
			fail(c, &service.Error{Status: http.StatusUnauthorized, Code: "UNAUTHENTICATED", Message: "Authentication is required."})
			c.Abort()
			return
		}
		if principal.Method == "google_oauth" && mutating(c.Request.Method) {
			supplied := c.GetHeader("X-CSRF-Token")
			if supplied == "" || subtle.ConstantTimeCompare([]byte(supplied), []byte(principal.CSRFToken)) != 1 {
				fail(c, &service.Error{Status: http.StatusForbidden, Code: "CSRF_INVALID", Message: "A valid CSRF token is required."})
				c.Abort()
				return
			}
		}
		c.Set(principalKey, principal)
		c.Next()
	}
}

func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := CurrentPrincipal(c)
		if principal == nil || principal.User.Role != domain.RoleAdmin {
			fail(c, &service.Error{Status: http.StatusForbidden, Code: "FORBIDDEN", Message: "Administrator access is required."})
			c.Abort()
			return
		}
		c.Next()
	}
}

func HoldRateLimit(client *redis.Client, cfg config.Config) gin.HandlerFunc {
	script := redis.NewScript(`local n=redis.call('INCR',KEYS[1]); if n==1 then redis.call('EXPIRE',KEYS[1],60) end; return n`)
	return func(c *gin.Context) {
		principal := CurrentPrincipal(c)
		if principal == nil {
			c.Next()
			return
		}
		key := "cinema:rate:hold:" + principal.User.ID.Hex() + ":" + c.ClientIP()
		count, err := script.Run(context.Background(), client, []string{key}).Int64()
		if err != nil {
			fail(c, &service.Error{Status: http.StatusServiceUnavailable, Code: "RATE_LIMIT_UNAVAILABLE", Message: "Request limiting is temporarily unavailable."})
			c.Abort()
			return
		}
		if count > int64(cfg.HoldRateLimit) {
			c.Header("Retry-After", "60")
			fail(c, &service.Error{Status: http.StatusTooManyRequests, Code: "RATE_LIMITED", Message: "Too many hold attempts."})
			c.Abort()
			return
		}
		c.Next()
	}
}

func CurrentPrincipal(c *gin.Context) *appauth.Principal {
	value, ok := c.Get(principalKey)
	if !ok {
		return nil
	}
	principal, _ := value.(*appauth.Principal)
	return principal
}
func bearer(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
		return strings.TrimSpace(parts[1])
	}
	return ""
}
func authenticate(c *gin.Context, authService *appauth.Service) (*appauth.Principal, error) {
	if token := bearer(c); token != "" {
		return authService.VerifyFirebase(c.Request.Context(), token)
	}
	cookie, err := c.Cookie(authService.CookieName())
	if err != nil {
		return nil, err
	}
	return authService.Session(c.Request.Context(), cookie)
}
func mutating(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch || method == http.MethodDelete
}
