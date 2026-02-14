package transport

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	tcpInitialBackoff = 2 * time.Second
	tcpMaxBackoff     = 60 * time.Second
	tcpDialTimeout    = 5 * time.Second
	tcpReadBufSize    = 4096
	tcpFrameChanSize  = 256
)

// TCPTransport connects to a Meshtastic device over TCP (default :4403).
// It uses Meshtastic's stream framing: 4-byte big-endian length prefix + payload.
type TCPTransport struct {
	addr    string
	log     *zap.Logger
	frames  chan ProtoFrame
	state   atomic.Int32 // ConnectionState
	mu      sync.Mutex
	conn    net.Conn
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

// NewTCPTransport constructs a TCPTransport and begins the connect loop.
func NewTCPTransport(addr string, log *zap.Logger) *TCPTransport {
	t := &TCPTransport{
		addr:   addr,
		log:    log,
		frames: make(chan ProtoFrame, tcpFrameChanSize),
	}
	t.state.Store(int32(StateDisconnected))
	return t
}

func (t *TCPTransport) Connect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if ConnectionState(t.state.Load()) == StateConnected {
		return nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	t.wg.Add(1)
	go t.readLoop(ctx)
	return nil
}

func (t *TCPTransport) Disconnect() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	if t.conn != nil {
		t.conn.Close()
		t.conn = nil
	}
	t.wg.Wait()
	t.state.Store(int32(StateDisconnected))
	return nil
}

func (t *TCPTransport) Send(frame ProtoFrame) error {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()

	if conn == nil {
		return fmt.Errorf("tcp: not connected")
	}
	// Meshtastic stream framing: 4-byte big-endian length + payload
	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(frame.Data)))
	if _, err := conn.Write(append(hdr, frame.Data...)); err != nil {
		return fmt.Errorf("tcp: send: %w", err)
	}
	return nil
}

func (t *TCPTransport) Receive() <-chan ProtoFrame { return t.frames }

func (t *TCPTransport) GetConnectionState() ConnectionState {
	return ConnectionState(t.state.Load())
}

// ── internal ──────────────────────────────────────────────────────────────

func (t *TCPTransport) readLoop(ctx context.Context) {
	defer t.wg.Done()

	backoff := tcpInitialBackoff
	for {
		if ctx.Err() != nil {
			t.state.Store(int32(StateDisconnected))
			return
		}

		t.state.Store(int32(StateConnecting))
		conn, err := net.DialTimeout("tcp", t.addr, tcpDialTimeout)
		if err != nil {
			t.log.Warn("tcp: dial failed",
				zap.String("addr", t.addr),
				zap.Duration("retry_in", backoff),
				zap.Error(err),
			)
			t.state.Store(int32(StateFailed))
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
				backoff = min(backoff*2, tcpMaxBackoff)
				continue
			}
		}

		backoff = tcpInitialBackoff
		t.mu.Lock()
		t.conn = conn
		t.mu.Unlock()
		t.state.Store(int32(StateConnected))
		t.log.Info("tcp: connected", zap.String("addr", t.addr))

		t.readFrames(ctx, conn)

		t.mu.Lock()
		t.conn = nil
		t.mu.Unlock()
		t.state.Store(int32(StateDisconnected))

		if ctx.Err() != nil {
			return
		}
		t.log.Info("tcp: connection lost, reconnecting",
			zap.Duration("backoff", backoff))
	}
}

func (t *TCPTransport) readFrames(ctx context.Context, conn net.Conn) {
	hdr := make([]byte, 4)
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()
	defer close(done)

	for {
		if _, err := io.ReadFull(conn, hdr); err != nil {
			if ctx.Err() == nil {
				t.log.Debug("tcp: read header", zap.Error(err))
			}
			return
		}
		n := binary.BigEndian.Uint32(hdr)
		if n == 0 || n > 512*1024 {
			t.log.Warn("tcp: invalid frame size", zap.Uint32("size", n))
			return
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(conn, payload); err != nil {
			if ctx.Err() == nil {
				t.log.Debug("tcp: read payload", zap.Error(err))
			}
			return
		}
		select {
		case t.frames <- ProtoFrame{Data: payload, Timestamp: time.Now().UTC()}:
		case <-ctx.Done():
			return
		default:
			t.log.Warn("tcp: frame channel full – dropping frame")
		}
	}
}

func min(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
