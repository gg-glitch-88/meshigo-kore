// Package gateway implements the Mesh Gateway Service.
// It owns the WebSocket event bus, REST API, and transport abstraction.
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/meshcommons/meshcommons/internal/api"
	"github.com/meshcommons/meshcommons/internal/config"
	"github.com/meshcommons/meshcommons/internal/store"
	"github.com/meshcommons/meshcommons/internal/transport"
)

// Gateway is the central application service.
type Gateway struct {
	cfg    *config.Config
	db     *store.DB
	log    *zap.Logger
	bus    *EventBus
	server *http.Server
}

// New constructs a Gateway without starting it.
func New(cfg *config.Config, db *store.DB, log *zap.Logger) *Gateway {
	bus := newEventBus()
	router := api.NewRouter(db, bus, log)

	srv := &http.Server{
		Addr:              cfg.Gateway.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return &Gateway{
		cfg:    cfg,
		db:     db,
		log:    log,
		bus:    bus,
		server: srv,
	}
}

// Start launches all subsystems and blocks until ctx is cancelled.
func (g *Gateway) Start(ctx context.Context) error {
	// Transport layer (BLE or TCP to Meshtastic)
	tr, err := transport.New(g.cfg, g.log)
	if err != nil {
		return fmt.Errorf("gateway: transport init: %w", err)
	}

	// Fan incoming mesh packets into the event bus and store.
	go g.ingestLoop(ctx, tr)

	ln, err := net.Listen("tcp", g.cfg.Gateway.ListenAddr)
	if err != nil {
		return fmt.Errorf("gateway: listen %s: %w", g.cfg.Gateway.ListenAddr, err)
	}
	g.log.Info("HTTP gateway listening", zap.String("addr", ln.Addr().String()))

	// Serve HTTP in background; shut down on ctx cancel.
	srvErr := make(chan error, 1)
	go func() {
		if err := g.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		g.log.Info("context cancelled – shutting down gateway")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return g.server.Shutdown(shutCtx)
	case err := <-srvErr:
		return err
	}
}

// ingestLoop reads packets from the transport and fans them out.
func (g *Gateway) ingestLoop(ctx context.Context, tr transport.Transport) {
	for {
		select {
		case <-ctx.Done():
			return
		case pkt, ok := <-tr.Packets():
			if !ok {
				return
			}
			msg := &store.Message{
				MeshID:     pkt.ID,
				FromNode:   pkt.From,
				ToNode:     pkt.To,
				Channel:    pkt.Channel,
				Payload:    pkt.Payload,
				ReceivedAt: time.Now().UTC(),
			}
			id, err := g.db.InsertMessage(msg)
			if err != nil {
				g.log.Warn("ingest: store message", zap.Error(err))
				continue
			}
			msg.ID = id
			g.bus.Publish(msg)
		}
	}
}
