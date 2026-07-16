package realtime

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/cinema-ticket-booking/backend/internal/domain"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
)

const channel = "cinema:seat-events"

type client struct {
	showtimeID string
	conn       *websocket.Conn
	send       chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	rooms   map[string]map[*client]struct{}
	redis   *redis.Client
	allowed map[string]struct{}
}

func NewHub(redisClient *redis.Client, origins []string) *Hub {
	allowed := map[string]struct{}{}
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	return &Hub{rooms: map[string]map[*client]struct{}{}, redis: redisClient, allowed: allowed}
}

func (h *Hub) Run(ctx context.Context) {
	pubsub := h.redis.Subscribe(ctx, channel)
	defer pubsub.Close()
	for {
		msg, err := pubsub.ReceiveMessage(ctx)
		if err != nil {
			if ctx.Err() == nil {
				slog.Error("seat-event subscription stopped", "error", err)
			}
			return
		}
		var event domain.SeatEvent
		if json.Unmarshal([]byte(msg.Payload), &event) != nil {
			continue
		}
		h.broadcast(event.ShowtimeID, []byte(msg.Payload))
	}
}

func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, showtimeID string, snapshot []byte) error {
	upgrader := websocket.Upgrader{ReadBufferSize: 1024, WriteBufferSize: 1024, CheckOrigin: func(req *http.Request) bool {
		origin := req.Header.Get("Origin")
		if origin == "" {
			return true
		}
		_, ok := h.allowed[origin]
		return ok
	}}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	c := &client{showtimeID: showtimeID, conn: conn, send: make(chan []byte, 32)}
	h.register(c)
	if len(snapshot) > 0 {
		c.send <- snapshot
	}
	go h.writePump(c)
	h.readPump(c)
	return nil
}

func (h *Hub) register(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.rooms[c.showtimeID] == nil {
		h.rooms[c.showtimeID] = map[*client]struct{}{}
	}
	h.rooms[c.showtimeID][c] = struct{}{}
}
func (h *Hub) unregister(c *client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room := h.rooms[c.showtimeID]
	if _, ok := room[c]; !ok {
		return
	}
	delete(room, c)
	close(c.send)
	_ = c.conn.Close()
	if len(room) == 0 {
		delete(h.rooms, c.showtimeID)
	}
}
func (h *Hub) broadcast(showtimeID string, payload []byte) {
	h.mu.RLock()
	clients := make([]*client, 0, len(h.rooms[showtimeID]))
	for c := range h.rooms[showtimeID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- payload:
		default:
			h.unregister(c)
		}
	}
}

func (h *Hub) readPump(c *client) {
	defer h.unregister(c)
	_ = c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error { return c.conn.SetReadDeadline(time.Now().Add(60 * time.Second)) })
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}
func (h *Hub) writePump(c *client) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case payload, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.conn.WriteMessage(websocket.TextMessage, payload) != nil {
				_ = c.conn.Close()
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if c.conn.WriteMessage(websocket.PingMessage, nil) != nil {
				_ = c.conn.Close()
				return
			}
		}
	}
}

func Snapshot(showtimeID string, seats any) []byte {
	event := domain.SeatEvent{EventID: uuid.NewString(), Type: "snapshot", ShowtimeID: showtimeID, OccurredAt: time.Now().UTC(), Data: map[string]any{"seats": seats}}
	payload, _ := json.Marshal(event)
	return payload
}
