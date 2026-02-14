// Package proto implements the Meshtastic protobuf encode/decode layer.
// It handles all five message types: POSITION_APP, TEXT_MESSAGE_APP,
// NODEINFO_APP, TELEMETRY_APP, and ROUTING_APP.
package proto

import (
	"encoding/binary"
	"fmt"

	goproto "google.golang.org/protobuf/proto"
)

// PortNum mirrors the Meshtastic PortNum enum.
type PortNum uint32

const (
	PortUnknown      PortNum = 0
	PortTextMessage  PortNum = 1  // TEXT_MESSAGE_APP
	PortPosition     PortNum = 3  // POSITION_APP
	PortNodeInfo     PortNum = 4  // NODEINFO_APP
	PortRouting      PortNum = 5  // ROUTING_APP
	PortTelemetry    PortNum = 67 // TELEMETRY_APP
)

// FromRadio is the top-level wrapper for data coming FROM the radio device.
// (Simplified: real Meshtastic uses a oneof; we use a union struct.)
type FromRadio struct {
	Packet    *MeshPacket
	NodeInfo  *NodeInfo
	MyInfo    *MyNodeInfo
	Telemetry *Telemetry
}

// ToRadio is the top-level wrapper for data going TO the radio device.
type ToRadio struct {
	Packet *MeshPacket
}

// MeshPacket is a routed Meshtastic packet.
type MeshPacket struct {
	ID       uint32
	From     uint32 // Source node number
	To       uint32 // Destination node number (0xFFFFFFFF = broadcast)
	Channel  uint32
	PortNum  PortNum
	Payload  []byte
	HopLimit uint32
	WantAck  bool
}

// NodeInfo carries metadata about a known mesh node.
type NodeInfo struct {
	NodeID      uint32
	LongName    string
	ShortName   string
	HardwareModel string
	Role        string
}

// MyNodeInfo carries this device's own identity.
type MyNodeInfo struct {
	MyNodeNum uint32
}

// Telemetry carries battery + signal metrics.
type Telemetry struct {
	BatteryLevel uint32  // percent 0–100
	Voltage      float32 // volts
	ChannelUtil  float32 // percent
	AirUtil      float32 // percent
}

// Position holds GPS coordinates from POSITION_APP packets.
type Position struct {
	LatitudeI  int32   // degrees × 1e-7
	LongitudeI int32   // degrees × 1e-7
	Altitude   int32   // metres
	Time       uint32  // unix seconds
	PDOP       uint32  // position dilution of precision
}

// ── Handler ───────────────────────────────────────────────────────────────

// MeshtasticProtobuf handles encode/decode of Meshtastic wire frames.
type MeshtasticProtobuf struct {
	opts goproto.MarshalOptions
}

// New returns a ready MeshtasticProtobuf handler.
func New() *MeshtasticProtobuf {
	return &MeshtasticProtobuf{
		opts: goproto.MarshalOptions{Deterministic: true},
	}
}

// DecodeFromRadio parses a raw 4-byte-prefixed frame received from the device.
// It validates structure and returns a typed FromRadio or an error.
func (m *MeshtasticProtobuf) DecodeFromRadio(data []byte) (*FromRadio, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("proto: frame too short (%d bytes)", len(data))
	}
	length := binary.BigEndian.Uint32(data[:4])
	payload := data[4:]
	if uint32(len(payload)) < length {
		return nil, fmt.Errorf("proto: frame truncated (want %d, got %d bytes)", length, len(payload))
	}

	// TODO: unmarshal real Meshtastic proto once github.com/meshtastic/go is wired.
	// For now, return a stub decoded packet so higher layers can be tested.
	fr := &FromRadio{
		Packet: &MeshPacket{
			Payload: append([]byte(nil), payload[:length]...),
			To:      0xFFFFFFFF, // broadcast
		},
	}
	return fr, nil
}

// EncodeToRadio serialises a ToRadio message into a framed byte slice.
func (m *MeshtasticProtobuf) EncodeToRadio(msg *ToRadio) ([]byte, error) {
	if msg == nil {
		return nil, fmt.Errorf("proto: cannot encode nil ToRadio")
	}
	if msg.Packet == nil {
		return nil, fmt.Errorf("proto: ToRadio.Packet must not be nil")
	}

	// TODO: marshal to real Meshtastic protobuf once github.com/meshtastic/go is wired.
	// For now, use the raw Payload bytes directly.
	payload := msg.Packet.Payload

	hdr := make([]byte, 4)
	binary.BigEndian.PutUint32(hdr, uint32(len(payload)))
	return append(hdr, payload...), nil
}

// MessageTypeLabel returns a human-readable label for a PortNum.
func MessageTypeLabel(p PortNum) string {
	switch p {
	case PortTextMessage:
		return "TEXT_MESSAGE_APP"
	case PortPosition:
		return "POSITION_APP"
	case PortNodeInfo:
		return "NODEINFO_APP"
	case PortTelemetry:
		return "TELEMETRY_APP"
	case PortRouting:
		return "ROUTING_APP"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", p)
	}
}
