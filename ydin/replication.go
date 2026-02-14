// Package replication implements peer discovery, content policy enforcement,
// and storage allocation for the mesh replication layer.
package replication

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/meshcommons/meshcommons/internal/config"
	"github.com/meshcommons/meshcommons/internal/store"
)

// Peer represents a known replication peer.
type Peer struct {
	NodeID      string
	DisplayName string
	LastSeen    time.Time
	Transport   string // "mesh" | "tcp" | "ble"
}

// Manager orchestrates peer discovery and content replication.
type Manager struct {
	cfg   *config.ReplicationConfig
	db    *store.DB
	log   *zap.Logger
	mu    sync.RWMutex
	peers map[string]*Peer
}

// New creates a Manager. Call Start to begin background work.
func New(cfg *config.ReplicationConfig, db *store.DB, log *zap.Logger) *Manager {
	return &Manager{
		cfg:   cfg,
		db:    db,
		log:   log,
		peers: make(map[string]*Peer),
	}
}

// Start launches discovery and sync loops; blocks until ctx is done.
func (m *Manager) Start(ctx context.Context) error {
	m.log.Info("replication manager starting",
		zap.Int("max_peers", m.cfg.MaxPeers),
		zap.Int64("storage_limit_bytes", m.cfg.StorageLimitBytes),
	)

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			m.log.Info("replication manager stopped")
			return nil
		case <-ticker.C:
			m.syncUnsyncedMessages()
		}
	}
}

// AddPeer registers or updates a peer's last-seen time.
func (m *Manager) AddPeer(p *Peer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.peers) >= m.cfg.MaxPeers {
		return fmt.Errorf("replication: peer limit (%d) reached", m.cfg.MaxPeers)
	}
	m.peers[p.NodeID] = p
	m.log.Info("peer registered", zap.String("node", p.NodeID))
	return nil
}

// RemovePeer deregisters a peer by node ID.
func (m *Manager) RemovePeer(nodeID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.peers, nodeID)
}

// PeerCount returns the number of currently known peers.
func (m *Manager) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// ── Content policy ────────────────────────────────────────────────────────

// AllowedToReplicate checks content policy for a given message.
// Currently: always allow. Extend with block-lists, size caps, etc.
func (m *Manager) AllowedToReplicate(msg *store.Message) bool {
	if int64(len(msg.Payload)) > m.cfg.StorageLimitBytes {
		m.log.Warn("replication: payload exceeds storage limit – skipping",
			zap.String("mesh_id", msg.MeshID),
			zap.Int("payload_bytes", len(msg.Payload)),
		)
		return false
	}
	return true
}

// ── Internal sync ─────────────────────────────────────────────────────────

func (m *Manager) syncUnsyncedMessages() {
	msgs, err := m.db.ListMessages(100)
	if err != nil {
		m.log.Error("replication: list messages", zap.Error(err))
		return
	}
	m.mu.RLock()
	peerCount := len(m.peers)
	m.mu.RUnlock()

	if peerCount == 0 {
		return
	}

	for _, msg := range msgs {
		if msg.Synced {
			continue
		}
		if !m.AllowedToReplicate(msg) {
			continue
		}
		// TODO: push msg.Payload to connected peers via transport.
		if err := m.db.MarkSynced(msg.ID); err != nil {
			m.log.Warn("replication: mark synced", zap.Error(err), zap.Int64("id", msg.ID))
		}
	}
}
