# MeshCommons

> Offline-first community platform running on a Raspberry Pi 4/5 (ARM64 · Debian 12 Bookworm · Kernel 6.1+)

```
Operating System (Debian 12 · ARM64 · Kernel 6.1+)
└── Infrastructure Services (hostapd · dnsmasq · systemd · BlueZ)
    └── Application Services
        ├── Mesh Gateway Service  ← Go (this repo)
        │   ├── Meshtastic protobuf handler
        │   ├── Transport abstraction (BLE / TCP)
        │   ├── WebSocket event bus
        │   ├── REST API
        │   └── State management (SQLite + WAL)
        ├── Web App (static)
        ├── Message Store (SQLite + WAL)
        ├── BitTorrent (qBittorrent / rtorrent)
        ├── Search Index (Sonic / Meilisearch)
        ├── Replication Manager (Go)
        └── Living Manual Wiki (DokuWiki / Gollum)
            └── Storage Layer (/var/lib/meshcommons/)
```

## Quick Start

```bash
# Install Go tools
make install-deps

# Build for current platform
make build

# Run with default config
make run

# Cross-compile for Raspberry Pi
make build-arm64
```

## Development

```bash
make fmt        # format
make vet        # static analysis
make lint       # golangci-lint
make test       # unit tests with race detector
make coverage   # HTML coverage report → bin/coverage.html
make bench      # benchmarks
```

## Project Layout

```
cmd/
  meshcommons/   main binary
  migrate/       standalone schema migration tool
configs/
  default.yaml   default JSON config
  meshcommons.service  systemd unit
internal/
  api/           REST + WebSocket handlers
  config/        configuration loading & validation
  gateway/       orchestrator + event bus
  proto/         generated (or stubbed) Meshtastic protobufs
  replication/   peer discovery + content policy
  search/        Sonic / Meilisearch abstraction
  store/         SQLite open + migrate + CRUD
  transport/     BLE / TCP transport abstraction
  version/       build-time metadata
  wiki/          Living Manual CRUD
proto/
  mesh.proto     Meshtastic protobuf definitions
```

## Storage

| Path | Purpose |
|---|---|
| `/var/lib/meshcommons/db/` | SQLite database |
| `/var/lib/meshcommons/files/` | File library |
| `/var/lib/meshcommons/torrents/` | Download cache |
| `/var/lib/meshcommons/wiki/` | Wiki pages (git-backed) |
| `/var/log/meshcommons/` | Logs |

## Deployment

```bash
# Copy binary
sudo cp bin/meshcommons /usr/local/bin/

# Install systemd service
sudo cp configs/meshcommons.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now meshcommons
```
