package observability

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
)

type Metrics struct {
	HTTPRequests        *prometheus.CounterVec
	HTTPDuration        *prometheus.HistogramVec
	Authentication      *prometheus.CounterVec
	LockConflicts       prometheus.Counter
	Holds               *prometheus.CounterVec
	Bookings            prometheus.Counter
	ActiveWebSockets    prometheus.Gauge
	ExpiredHolds        prometheus.Counter
	OutboxPublished     prometheus.Counter
	OutboxFailures      prometheus.Counter
	OutboxLag           prometheus.Gauge
	NotificationRetries prometheus.Counter
	DLQMessages         prometheus.Counter
}

func NewMetrics() *Metrics {
	m := &Metrics{
		HTTPRequests:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cinema_http_requests_total", Help: "HTTP requests."}, []string{"method", "route", "status"}),
		HTTPDuration:        prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "cinema_http_request_duration_seconds", Help: "HTTP request latency."}, []string{"method", "route"}),
		Authentication:      prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cinema_authentication_total", Help: "Authentication checks by method and result."}, []string{"method", "result"}),
		LockConflicts:       prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_seat_lock_conflicts_total", Help: "Seat lock conflicts."}),
		Holds:               prometheus.NewCounterVec(prometheus.CounterOpts{Name: "cinema_holds_total", Help: "Hold transitions."}, []string{"result"}),
		Bookings:            prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_bookings_total", Help: "Confirmed bookings."}),
		ActiveWebSockets:    prometheus.NewGauge(prometheus.GaugeOpts{Name: "cinema_websocket_connections", Help: "Active WebSocket connections."}),
		ExpiredHolds:        prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_expired_holds_total", Help: "Expired holds."}),
		OutboxPublished:     prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_outbox_published_total", Help: "Published outbox events."}),
		OutboxFailures:      prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_outbox_failures_total", Help: "Outbox publish failures."}),
		OutboxLag:           prometheus.NewGauge(prometheus.GaugeOpts{Name: "cinema_outbox_lag_seconds", Help: "Age of the oldest unpublished outbox event."}),
		NotificationRetries: prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_notification_retries_total", Help: "Notification retries."}),
		DLQMessages:         prometheus.NewCounter(prometheus.CounterOpts{Name: "cinema_notification_dlq_total", Help: "Dead-lettered notifications."}),
	}
	for _, method := range []string{"firebase", "google_oauth"} {
		for _, result := range []string{"success", "failure"} {
			m.Authentication.WithLabelValues(method, result).Add(0)
		}
	}
	prometheus.MustRegister(m.HTTPRequests, m.HTTPDuration, m.Authentication, m.LockConflicts, m.Holds, m.Bookings, m.ActiveWebSockets, m.ExpiredHolds, m.OutboxPublished, m.OutboxFailures, m.OutboxLag, m.NotificationRetries, m.DLQMessages)
	return m
}

func (m *Metrics) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		m.HTTPRequests.WithLabelValues(c.Request.Method, route, strconv.Itoa(c.Writer.Status())).Inc()
		m.HTTPDuration.WithLabelValues(c.Request.Method, route).Observe(time.Since(start).Seconds())
	}
}
