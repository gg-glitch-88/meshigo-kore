// Package api implements the REST API Server for MeshCommons.
//
// Routes (spec-exact):
//   GET  /api/v1/nodes              — List all known nodes
//   GET  /api/v1/nodes/:id          — Single node detail
//   GET  /api/v1/messages           — Message history (paginated)
//   POST /api/v1/messages           — Send new message
//   GET  /api/v1/channels           — Channel list
//   GET  /api/v1/status             — Gateway health
//   POST /api/v1/checkin            — User check-in
//   GET  /api/v1/library/search     — Search library
//   GET  /api/v1/library/files      — Browse files
//   GET  /api/v1/events             — WebSocket live stream
//
// Framework: standard library net/http with chi router for middleware.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/gg-glitch-88/meshigo-kore/ydin/state"
	"github.com/gg-glitch-88/meshigo-kore/ydin/store"
)

// EventBus is the subset of gateway.EventBus the API needs.
type EventBus interface {
	Subscribe() (<-chan interface{}, func())
}

// GatewayEventBus matches the concrete EventBus without importing gateway.
type GatewayEventBus interface {
	Subscribe() (<-chan interface{}, func())
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
	CheckOrigin:     func(_ *http.Request) bool { return true },
}

// SubscribeFunc is the adapter the API uses to subscribe to any event bus.
type SubscribeFunc func() (<-chan interface{}, func())

// Server holds handler dependencies.
type Server struct {
	db          *store.DB
	stateMgr    *state.Manager
	subscribeFn func() (<-chan interface{}, func())
	log         *zap.Logger
}

// NewRouter wires all /api/v1/* routes and returns a http.Handler.
// subFn is called for each new WebSocket client; it must return a channel
// of JSON-serialisable events and an unsubscribe function.
func NewRouter(
	db *store.DB,
	stateMgr *state.Manager,
	subFn func() (<-chan interface{}, func()),
	log *zap.Logger,
) http.Handler {
	s := &Server{db: db, stateMgr: stateMgr, subscribeFn: subFn, log: log}

	mux := http.NewServeMux()

	// Nodes
	mux.HandleFunc("GET /api/v1/nodes", s.listNodes)
	mux.HandleFunc("GET /api/v1/nodes/{id}", s.getNode)

	// Messages
	mux.HandleFunc("GET /api/v1/messages", s.listMessages)
	mux.HandleFunc("POST /api/v1/messages", s.sendMessage)

	// Channels
	mux.HandleFunc("GET /api/v1/channels", s.listChannels)

	// Status / health
	mux.HandleFunc("GET /api/v1/status", s.status)

	// Check-in
	mux.HandleFunc("POST /api/v1/checkin", s.checkin)

	// Library
	mux.HandleFunc("GET /api/v1/library/search", s.librarySearch)
	mux.HandleFunc("GET /api/v1/library/files", s.libraryFiles)

	// WebSocket event stream
	mux.HandleFunc("GET /api/v1/events", s.eventStream)

	return withLogging(log, mux)
}

// ── Nodes ─────────────────────────────────────────────────────────────────

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes := s.stateMgr.ListNodes()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	// Accept both decimal and "!hex" formats.
	var nodeID uint32
	if strings.HasPrefix(idStr, "!") {
		fmt.Sscanf(idStr, "!%x", &nodeID) //nolint:errcheck
	} else {
		n, err := strconv.ParseUint(idStr, 10, 32)
		if err != nil {
			http.Error(w, "invalid node id", http.StatusBadRequest)
			return
		}
		nodeID = uint32(n)
	}

	node, ok := s.stateMgr.GetNode(nodeID)
	if !ok {
		http.Error(w, "node not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, node)
}

// ── Messages ──────────────────────────────────────────────────────────────

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) {
	limit, err := queryInt(r, "limit", 50, 1, 500)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	msgs, err := s.db.ListMessages(limit)
	if err != nil {
		s.log.Error("api: list messages", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"messages": msgs,
		"count":    len(msgs),
	})
}

type sendMessageRequest struct {
	Text    string `json:"text"`
	Channel int    `json:"channel"`
	ToNode  string `json:"to_node"`
}

func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request) {
	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Text) == "" {
		http.Error(w, "text must not be empty", http.StatusBadRequest)
		return
	}
	toNode := req.ToNode
	if toNode == "" {
		toNode = "broadcast"
	}
	msg := &store.Message{
		MeshID:     fmt.Sprintf("api-%d", time.Now().UnixNano()),
		FromNode:   "gateway",
		ToNode:     toNode,
		Channel:    req.Channel,
		Payload:    []byte(req.Text),
		ReceivedAt: time.Now().UTC(),
	}
	id, err := s.db.InsertMessage(msg)
	if err != nil {
		s.log.Error("api: send message", zap.Error(err))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "status": "queued"})
}

// ── Channels ──────────────────────────────────────────────────────────────

type channel struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Role  string `json:"role"` // "PRIMARY" | "SECONDARY" | "DISABLED"
}

func (s *Server) listChannels(w http.ResponseWriter, r *http.Request) {
	// Static channel list; a future version reads from device config.
	channels := []channel{
		{Index: 0, Name: "LongFast", Role: "PRIMARY"},
		{Index: 1, Name: "Admin", Role: "SECONDARY"},
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"channels": channels})
}

// ── Status ────────────────────────────────────────────────────────────────

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":      "ok",
		"time":        time.Now().UTC().Format(time.RFC3339),
		"node_count":  s.stateMgr.NodeCount(),
		"subscribers": 0, // TODO: expose EventBus.Len() via interface
	})
}

// ── Check-in ──────────────────────────────────────────────────────────────

type checkinRequest struct {
	NodeID   string `json:"node_id"`
	Location string `json:"location,omitempty"`
}

func (s *Server) checkin(w http.ResponseWriter, r *http.Request) {
	var req checkinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.NodeID == "" {
		http.Error(w, "node_id required", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"checked_in": true,
		"node_id":    req.NodeID,
		"time":       time.Now().UTC().Format(time.RFC3339),
	})
}

// ── Library ───────────────────────────────────────────────────────────────

func (s *Server) librarySearch(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		http.Error(w, "q parameter required", http.StatusBadRequest)
		return
	}
	// TODO: delegate to search.Backend.Search("library", q, 20)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"query":   q,
		"results": []interface{}{},
	})
}

func (s *Server) libraryFiles(w http.ResponseWriter, r *http.Request) {
	// TODO: enumerate /var/lib/meshcommons/files/
	writeJSON(w, http.StatusOK, map[string]interface{}{"files": []interface{}{}})
}

// ── WebSocket event stream ────────────────────────────────────────────────

func (s *Server) eventStream(w http.ResponseWriter, r *http.Request) {
	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("api: ws upgrade", zap.Error(err))
		return
	}
	defer conn.Close()

	ch, unsub := s.subscribeFn()
	defer unsub()

	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()

	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteJSON(evt); err != nil {
				s.log.Debug("api: ws write", zap.Error(err))
				return
			}
		case <-ping.C:
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-r.Context().Done():
			return
		}
	}
}

// ── Middleware ────────────────────────────────────────────────────────────

func withLogging(log *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Debug("api",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", rw.code),
			zap.Duration("duration", time.Since(start)),
		)
	})
}

type responseWriter struct {
	http.ResponseWriter
	code int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.code = code
	rw.ResponseWriter.WriteHeader(code)
}

// ── helpers ───────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func queryInt(r *http.Request, key string, def, min, max int) (int, error) {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < min || n > max {
		return 0, fmt.Errorf("%s must be %d–%d", key, min, max)
	}
	return n, nil
}
