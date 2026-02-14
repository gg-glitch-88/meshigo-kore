// Package state implements the StateManager for node metadata and message history.
// It keeps a hot in-memory index (map) and persists via the store package.
package state

import (
	"fmt"
	"sync"
	"time"

	"github.com/gg-glitch-88/meshigo-kore/ydin/store"
)

// Node is a known mesh participant.
type Node struct {
	NodeID    uint32
	NodeIDHex string // e.g. "!deadbeef"
	LongName  string
	ShortName string
	Hardware  string
	Role      string
	LastSeen  time.Time
	// Latest telemetry
	BatteryLevel uint32
	Voltage      float32
	// Latest position
	Lat float64
	Lon float64
	Alt int32
}

// Manager holds all runtime state: known nodes + recent messages.
// All exported methods are safe for concurrent use.
type Manager struct {
	db    *store.DB
	mu    sync.RWMutex
	nodes map[uint32]*Node // keyed by numeric node ID
}

// New creates a Manager and hydrates the node cache from the database.
func New(db *store.DB) (*Manager, error) {
	m := &Manager{
		db:    db,
		nodes: make(map[uint32]*Node),
	}
	if err := m.loadNodes(); err != nil {
		return nil, fmt.Errorf("state: load nodes: %w", err)
	}
	return m, nil
}

// ── Node state ────────────────────────────────────────────────────────────

// UpsertNode creates or refreshes a node in both memory and the database.
func (m *Manager) UpsertNode(n *Node) error {
	if n.NodeID == 0 {
		return fmt.Errorf("state: node ID must not be zero")
	}
	n.LastSeen = time.Now().UTC()
	if n.NodeIDHex == "" {
		n.NodeIDHex = fmt.Sprintf("!%08x", n.NodeID)
	}

	m.mu.Lock()
	m.nodes[n.NodeID] = n
	m.mu.Unlock()

	// Persist to SQLite
	_, err := m.db.Exec(`
		INSERT INTO peers (node_id, display_name, last_seen, transport)
		VALUES (?, ?, ?, 'mesh')
		ON CONFLICT(node_id) DO UPDATE
		  SET display_name = excluded.display_name,
		      last_seen    = excluded.last_seen`,
		n.NodeIDHex, n.LongName, n.LastSeen.Unix(),
	)
	return err
}

// GetNode retrieves a node by numeric ID.
func (m *Manager) GetNode(nodeID uint32) (*Node, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	n, ok := m.nodes[nodeID]
	return n, ok
}

// ListNodes returns a snapshot of all known nodes.
func (m *Manager) ListNodes() []*Node {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*Node, 0, len(m.nodes))
	for _, n := range m.nodes {
		copy := *n
		out = append(out, &copy)
	}
	return out
}

// NodeCount returns how many nodes are currently known.
func (m *Manager) NodeCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.nodes)
}

// UpdateTelemetry updates battery and signal data for a node.
func (m *Manager) UpdateTelemetry(nodeID uint32, battery uint32, voltage, chanUtil float32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[nodeID]; ok {
		n.BatteryLevel = battery
		n.Voltage = voltage
		n.LastSeen = time.Now().UTC()
	}
}

// UpdatePosition updates GPS coordinates for a node.
func (m *Manager) UpdatePosition(nodeID uint32, lat, lon float64, alt int32) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n, ok := m.nodes[nodeID]; ok {
		n.Lat = lat
		n.Lon = lon
		n.Alt = alt
		n.LastSeen = time.Now().UTC()
	}
}

// ── Message state ─────────────────────────────────────────────────────────

// RecordMessage persists a decoded text message.
func (m *Manager) RecordMessage(msg *store.Message) (int64, error) {
	return m.db.InsertMessage(msg)
}

// RecentMessages returns the n most recent messages.
func (m *Manager) RecentMessages(n int) ([]*store.Message, error) {
	return m.db.ListMessages(n)
}

// ── internal ──────────────────────────────────────────────────────────────

func (m *Manager) loadNodes() error {
	rows, err := m.db.Query(
		`SELECT node_id, display_name, last_seen FROM peers`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			nodeIDHex   string
			displayName string
			lastSeenTS  int64
		)
		if err := rows.Scan(&nodeIDHex, &displayName, &lastSeenTS); err != nil {
			return err
		}
		var nodeNum uint32
		fmt.Sscanf(nodeIDHex, "!%x", &nodeNum) //nolint:errcheck
		m.nodes[nodeNum] = &Node{
			NodeID:    nodeNum,
			NodeIDHex: nodeIDHex,
			LongName:  displayName,
			LastSeen:  time.Unix(lastSeenTS, 0),
		}
	}
	return rows.Err()
}
