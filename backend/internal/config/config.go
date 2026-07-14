package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	AppEnv              string
	HTTPPort            string
	WorkerMetricsPort   string
	FrontendURL         string
	AllowedOrigins      []string
	MongoURI            string
	MongoDatabase       string
	RedisAddr           string
	RedisPassword       string
	RedisDB             int
	SeatLockTTL         time.Duration
	HoldSweepInterval   time.Duration
	RabbitURL           string
	RabbitExchange      string
	RabbitQueue         string
	FirebaseProjectID   string
	FirebaseClientEmail string
	FirebasePrivateKey  string
	GoogleClientID      string
	GoogleClientSecret  string
	GoogleRedirectURL   string
	AdminEmails         map[string]struct{}
	SessionCookieName   string
	SessionTTL          time.Duration
	OAuthStateTTL       time.Duration
	CookieSecure        bool
	HoldRateLimit       int
	LogLevel            string
	OTLPEndpoint        string
	OTLPInsecure        bool
	TracesEnabled       bool
}

func Load() (Config, error) {
	seatTTL, err := duration("SEAT_LOCK_TTL", 5*time.Minute)
	if err != nil {
		return Config{}, err
	}
	sweep, err := duration("HOLD_SWEEP_INTERVAL", 5*time.Second)
	if err != nil {
		return Config{}, err
	}
	sessionTTL, err := duration("SESSION_TTL", 12*time.Hour)
	if err != nil {
		return Config{}, err
	}
	stateTTL, err := duration("OAUTH_STATE_TTL", 10*time.Minute)
	if err != nil {
		return Config{}, err
	}
	db, err := integer("REDIS_DB", 0)
	if err != nil {
		return Config{}, err
	}
	rate, err := integer("HOLD_RATE_LIMIT", 10)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		AppEnv:              env("APP_ENV", "development"),
		HTTPPort:            env("HTTP_PORT", "8080"),
		WorkerMetricsPort:   env("WORKER_METRICS_PORT", "9091"),
		FrontendURL:         strings.TrimRight(env("FRONTEND_URL", "http://localhost:3000"), "/"),
		AllowedOrigins:      csv(env("ALLOWED_ORIGINS", "http://localhost:3000")),
		MongoURI:            env("MONGODB_URI", "mongodb://localhost:27017/?replicaSet=rs0"),
		MongoDatabase:       env("MONGODB_DATABASE", "cinema"),
		RedisAddr:           env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:       os.Getenv("REDIS_PASSWORD"),
		RedisDB:             db,
		SeatLockTTL:         seatTTL,
		HoldSweepInterval:   sweep,
		RabbitURL:           env("RABBITMQ_URL", "amqp://guest:guest@localhost:5672/"),
		RabbitExchange:      env("RABBITMQ_EXCHANGE", "cinema.events"),
		RabbitQueue:         env("RABBITMQ_QUEUE", "booking.notifications"),
		FirebaseProjectID:   os.Getenv("FIREBASE_PROJECT_ID"),
		FirebaseClientEmail: os.Getenv("FIREBASE_CLIENT_EMAIL"),
		FirebasePrivateKey:  strings.ReplaceAll(os.Getenv("FIREBASE_PRIVATE_KEY"), `\n`, "\n"),
		GoogleClientID:      os.Getenv("GOOGLE_OAUTH_CLIENT_ID"),
		GoogleClientSecret:  os.Getenv("GOOGLE_OAUTH_CLIENT_SECRET"),
		GoogleRedirectURL:   env("GOOGLE_OAUTH_REDIRECT_URL", "http://localhost:3000/api/v1/auth/google/callback"),
		AdminEmails:         emailSet(os.Getenv("ADMIN_EMAILS")),
		SessionCookieName:   env("SESSION_COOKIE_NAME", "cinema_session"),
		SessionTTL:          sessionTTL,
		OAuthStateTTL:       stateTTL,
		CookieSecure:        boolean("COOKIE_SECURE", false),
		HoldRateLimit:       rate,
		LogLevel:            env("LOG_LEVEL", "info"),
		OTLPEndpoint:        env("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317"),
		OTLPInsecure:        boolean("OTEL_EXPORTER_OTLP_INSECURE", true),
		TracesEnabled:       boolean("OTEL_TRACES_ENABLED", true),
	}
	if cfg.SeatLockTTL <= 0 || cfg.HoldSweepInterval <= 0 || cfg.SessionTTL <= 0 || cfg.OAuthStateTTL <= 0 {
		return Config{}, errors.New("duration settings must be positive")
	}
	if cfg.AppEnv == "production" && !cfg.CookieSecure {
		return Config{}, errors.New("COOKIE_SECURE must be true in production")
	}
	return cfg, nil
}

func (c Config) FirebaseConfigured() bool {
	return c.FirebaseProjectID != "" && c.FirebaseClientEmail != "" && c.FirebasePrivateKey != ""
}
func (c Config) GoogleConfigured() bool {
	return c.GoogleClientID != "" && c.GoogleClientSecret != "" && c.GoogleRedirectURL != ""
}
func (c Config) IsAdmin(email string) bool {
	_, ok := c.AdminEmails[strings.ToLower(strings.TrimSpace(email))]
	return ok
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func csv(v string) []string {
	var out []string
	for _, x := range strings.Split(v, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
func emailSet(v string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, e := range csv(v) {
		out[strings.ToLower(e)] = struct{}{}
	}
	return out
}
func duration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return time.ParseDuration(v)
}
func integer(key string, fallback int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return strconv.Atoi(v)
}
func boolean(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
