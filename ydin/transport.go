// Package transport provides the TransportManager interface and BLE/TCP implementations
// that connect to a Meshtastic/Heltec device.
package transport

import (
	"time"
)

// ConnectionState describes the current link status.
type ConnectionState int

const (
	StateDisconnected ConnectionState = iota
	StateConnecting
	StateConnected
	StateFailed
)

func (s ConnectionState) String() string {
	switch s {
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	case StateFailed:
		return "failed"
	default:
		return "disconnected"
	}
}

// ProtoFrame is a raw Meshtastic framed payload (varint-length-prefixed bytes).
type ProtoFrame struct {
	Data      []byte
	Timestamp time.Time
}

// TransportManager is the abstraction over BLE and TCP transports.
// Implementations must be safe for concurrent use.
type TransportManager interface {
	// Connect establishes the physical link. Idempotent if already connected.
	Connect() error
	// Disconnect tears down the link gracefully.
	Disconnect() error
	// Send encodes and writes a frame to the device.
	Send(frame ProtoFrame) error
	// Receive returns a channel of inbound frames. Closed when disconnected.
	Receive() <-chan ProtoFrame
	// GetConnectionState returns the current link state.
	GetConnectionState() ConnectionState
}
