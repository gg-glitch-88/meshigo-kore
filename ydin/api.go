// Package api exposes the REST API and WebSocket event stream.
package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/meshcommons/meshcommons/internal/store"
)

// EventBus is the subset of gateway.EventBus that api needs.
type EventBus interface {
	Subscribe() (<-chan *store.Message, func())
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}

// NewRouter wires all routes onto a standard ServeMux.
func NewRouter(db *store.DB, bus EventBus, log *zap.Logger) http.Handler {
	mux := http.NewServeMux()

	h := &handlers{db: db, bus: bus, log: log}

	// Health check
	mux.HandleFunc("GET /api/health", h.health)
	// Message history
	mux.HandleFunc("GET /api/messages", h.listMessages)
	// WebSocket live stream
	mux.HandleFunc("GET /api/events", h.eventStream)

	return mux
}

type handlers struct {
	db  *store.DB
	bus EventBus
	log *zap.Logger
}

// ── /api/health ───────────────────────────────────────────────────────────

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

// ── /api/messages ─────────────────────────────────────────────────────────

func (h *handlers) listMessages(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 1 || n > 500 {
			http.Error(w, "limit must be 1–500", http.StatusBadRequest)
			return
		}
		limit = n
	}

	msgs, err := h.db.ListMessages(limit)
	if err != nil {
		h.log.Error("api: list messages", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type msgDTO struct {
		ID         int64  `json:"id"`
		MeshID     string `json:"mesh_id"`
		FromNode   string `json:"from_node"`
		ToNode     string `json:"to_node"`
		Channel    int    `json:"channel"`
		ReceivedAt string `json:"received_at"`
		Synced     bool   `json:"synced"`
	}
	out := make([]msgDTO, len(msgs))
	for i, m := range msgs {
		out[i] = msgDTO{
			ID:         m.ID,
			MeshID:     m.MeshID,
			FromNode:   m.FromNode,
			ToNode:     m.ToNode,
			Channel:    m.Channel,
			ReceivedAt: m.ReceivedAt.Format(time.RFC3339Nano),
			Synced:     m.Synced,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// ── /api/events (WebSocket) ───────────────────────────────────────────────

func (h *handlers) eventStream(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("api: ws upgrade", zap.Error(err))
		return
	}
	defer conn.Close()

	ch, unsub := h.bus.Subscribe()
	defer unsub()

	// Ping loop to keep the connection alive.
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(msg); err != nil {
				h.log.Debug("api: ws write", zap.Error(err))
				return
			}
		case <-ticker.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}
