// Package gateway implements the Mesh Gateway Service (Core).
// It wires all sub-components together:
//   - TransportManager  — BLE + TCP abstraction
//   - MeshtasticProtobuf — protobuf encode/decode
//   - EventBus          — WebSocket fan-out
//   - StateManager      — node + message state
//   - RESTServer        — HTTP API
package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/gg-glitch-88/meshigo-kore/ydin/api"
	"github.com/gg-glitch-88/meshigo-kore/ydin/config"
	meshproto "github.com/gg-glitch-88/meshigo-kore/ydin/proto"
	"github.com/gg-glitch-88/meshigo-kore/ydin/state"
	"github.com/gg-glitch-88/meshigo-kore/ydin/store"
	"github.com/gg-glitch-88/meshigo-kore/ydin/transport"
)

// GatewayService is the central application service.
// Struct mirrors the spec exactly:
//
//	type GatewayService struct {
//	    transport    TransportManager   // Handles BLE + TCP abstraction
//	    protoHandler MeshtasticProtobuf // Protobuf encode/decode
//	    eventBus     EventBus           // WebSocket fanout
//	    stateStore   StateManager       // Node + message state
//	    apiServer    RESTServer         // HTTP API
//	    config       Config             // Runtime config
//	}
type GatewayService struct {
	transport    transport.TransportManager
	protoHandler *meshproto.MeshtasticProtobuf
	eventBus     *EventBus
	stateStore   *state.Manager
	apiServer    *http.Server
	config       *config.Config
	log          *zap.Logger
}

// New constructs a GatewayService but does not start it.
func New(cfg *config.Config, db *store.DB, log *zap.Logger) (*GatewayService, error) {
	stateMgr, err := state.New(db)
	if err != nil {
		return nil, fmt.Errorf("gateway: state manager: %w", err)
	}

	bus := NewEventBus()

	// Adapter: wrap typed Event channel for the API's interface{} subscribe signature.
	subFn := func() (<-chan interface{}, func()) {
		typedCh, unsub := bus.Subscribe()
		ch := make(chan interface{}, 64)
		go func() {
			defer close(ch)
			for e := range typedCh {
				ch <- e
			}
		}()
		return ch, unsub
	}

	router := api.NewRouter(db, stateMgr, subFn, log)

	srv := &http.Server{
		Addr:              cfg.Gateway.ListenAddr,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	tr := transport.New(cfg, log)

	return &GatewayService{
		transport:    tr,
		protoHandler: meshproto.New(),
		eventBus:     bus,
		stateStore:   stateMgr,
		apiServer:    srv,
		config:       cfg,
		log:          log,
	}, nil
}

// Start launches all subsystems and blocks until ctx is cancelled.
func (g *GatewayService) Start(ctx context.Context) error {
	// Connect transport (non-fatal – will retry in background).
	if err := g.transport.Connect(); err != nil {
		g.log.Warn("gateway: initial transport connect failed – will retry",
			zap.Error(err))
	}

	go g.ingestLoop(ctx)

	ln, err := net.Listen("tcp", g.config.Gateway.ListenAddr)
	if err != nil {
		return fmt.Errorf("gateway: listen %s: %w", g.config.Gateway.ListenAddr, err)
	}
	g.log.Info("HTTP gateway listening", zap.String("addr", ln.Addr().String()))

	srvErr := make(chan error, 1)
	go func() {
		if err := g.apiServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		g.log.Info("gateway: shutting down")
		g.transport.Disconnect() //nolint:errcheck
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return g.apiServer.Shutdown(shutCtx)
	case err := <-srvErr:
		return err
	}
}

// ingestLoop reads decoded frames from the transport, stores them,
// and publishes them to the event bus.
func (g *GatewayService) ingestLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-g.transport.Receive():
			if !ok {
				return
			}
			fr, err := g.protoHandler.DecodeFromRadio(
				append([]byte{0, 0, 0, 0}, frame.Data...), // re-add length prefix stripped by TCP
			)
			if err != nil {
				g.log.Warn("gateway: decode frame", zap.Error(err))
				continue
			}
			if fr.Packet == nil {
				continue
			}

			msg := &store.Message{
				MeshID:     fmt.Sprintf("%d", fr.Packet.ID),
				FromNode:   fmt.Sprintf("!%08x", fr.Packet.From),
				ToNode:     fmt.Sprintf("!%08x", fr.Packet.To),
				Channel:    int(fr.Packet.Channel),
				Payload:    fr.Packet.Payload,
				ReceivedAt: frame.Timestamp,
			}
			id, err := g.stateStore.RecordMessage(msg)
			if err != nil {
				g.log.Warn("gateway: store message", zap.Error(err))
				continue
			}
			msg.ID = id

			g.eventBus.PublishMessage(msg)
		}
	}
}

// EventBusLen exposes subscriber count for testing/metrics.
func (g *GatewayService) EventBusLen() int { return g.eventBus.Len() }
