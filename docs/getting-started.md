# Getting Started

## Prerequisites

- **Go 1.21+** — [install](https://golang.org/dl/)
- **Docker** (optional) — for containerised deployments
- **curl** — for manual API testing

The scheduler embeds DefraDB and needs no external database.

## Configuration

Copy the default config and set the required fields:

```bash
cp config/config.yaml config/local.yaml
```

The minimum required settings:

| Field | Config key | Environment variable |
|---|---|---|
| Chain identifier | `scheduler.chain` | `SCHEDULER_CHAIN` |
| Network identifier | `scheduler.network` | `SCHEDULER_NETWORK` |
| HMAC secret | `scheduler.auth.hmac_secret` | `SCHEDULER_HMAC_SECRET` |
| DefraDB keyring | `defradb.keyring_secret` | `DEFRA_KEYRING_SECRET` |

Environment variables override the config file values. For local development the env vars approach is the simplest:

```bash
export DEFRA_KEYRING_SECRET=dev-secret
export SCHEDULER_HMAC_SECRET=dev-secret
export SCHEDULER_CHAIN=ethereum
export SCHEDULER_NETWORK=testnet
```

Key optional settings (defaults are shown in [config/config.yaml](../config/config.yaml)):

| Key | Default | Description |
|---|---|---|
| `scheduler.server.port` | `8090` | HTTP listen port |
| `scheduler.probe.interval_seconds` | `60` | Health probe sweep interval |
| `scheduler.probe.staleness_window_seconds` | `120` | Heartbeat age limit before discovery exclusion |
| `scheduler.diversity.enabled` | `true` | Match-history-based diversity weighting |
| `scheduler.accounting.enabled` | `false` | Enable delivery verification subsystem |
| `scheduler.settlement.enabled` | `false` | Enable escrow and batch settlement |

## Running

### Local (Go toolchain)

```bash
DEFRA_KEYRING_SECRET=dev-secret \
SCHEDULER_HMAC_SECRET=dev-secret \
SCHEDULER_CHAIN=ethereum \
SCHEDULER_NETWORK=testnet \
  go run ./cmd/scheduler --config config/config.yaml
```

The scheduler writes its embedded database to `.scheduler/defradb` by default (set by `defradb.store.path`).

### Docker

```bash
docker build -t shinzo-scheduler .

docker run -p 8090:8090 \
  -e DEFRA_KEYRING_SECRET=dev-secret \
  -e SCHEDULER_HMAC_SECRET=dev-secret \
  -e SCHEDULER_CHAIN=ethereum \
  -e SCHEDULER_NETWORK=testnet \
  shinzo-scheduler
```

### Docker Compose

```bash
DEFRA_KEYRING_SECRET=dev-secret \
SCHEDULER_HMAC_SECRET=dev-secret \
SCHEDULER_CHAIN=ethereum \
SCHEDULER_NETWORK=testnet \
  docker compose up
```

## Verifying the setup

Once running, confirm the scheduler is healthy:

```bash
curl http://localhost:8090/v1/health
# {"status":"ok","indexers":0,"hosts":0,"subscriptions":0}

curl http://localhost:8090/v1/stats
# Returns counts of active indexers, hosts, and subscriptions
```

## Testing

### Unit tests

```bash
go test ./...
```

With coverage:

```bash
go test ./... -coverprofile=coverage.out
go tool cover -func=coverage.out | grep total
```

### Integration tests

Integration tests spin up a real DefraDB instance in a temporary directory. They require the `integration` build tag:

```bash
go test -tags=integration ./test/integration/ -v
```

These tests exercise end-to-end use cases — registration, discovery, subscriptions, accounting, and settlement — against a live in-process scheduler.

## Manual testing with shell scripts

The [docs/scripts/](scripts/) directory contains ready-to-run scripts for the most common topologies. Each script:

1. Generates real secp256k1 peer identities using the [`gen-peer-identity`](scripts/gen-peer-identity/) tool
2. Registers indexers and hosts against a running scheduler
3. Exercises discovery, subscription creation, payment verification, and cancellation

Prerequisites for running the scripts:

```bash
# Scheduler must be running on localhost:8090
# SCHEDULER_CHAIN and SCHEDULER_NETWORK must match the values in config
export SCHEDULER_CHAIN=ethereum
export SCHEDULER_NETWORK=testnet

# jq must be installed for JSON parsing
brew install jq   # macOS
apt install jq    # Debian/Ubuntu
```

Available scripts:

| Script | Topology | Description |
|---|---|---|
| [`01-basic.sh`](scripts/01-basic.sh) | 1 indexer + 1 host | Register, discover, subscribe, activate, cancel |
| [`02-two-indexers.sh`](scripts/02-two-indexers.sh) | 2 indexers + 1 host | Parallel discovery, subscribe to best candidate |
| [`03-two-hosts.sh`](scripts/03-two-hosts.sh) | 1 indexer + 2 hosts | Independent subscriptions per host |

Run any script directly:

```bash
bash docs/scripts/01-basic.sh
```

## Bootstrap peers

For automated deployments, pre-seed known indexers in `config.yaml` so they are registered on startup without an API call:

```yaml
scheduler:
  bootstrap:
    indexers:
      - peer_id: "QmYourIndexerPeerID"
        http_url: "https://indexer.example.com"
        multiaddr: "/ip4/1.2.3.4/tcp/9000/p2p/QmYourIndexerPeerID"
```

The scheduler logs the issued API key for each bootstrap peer at startup:
```
bootstrap indexer seeded: peer_id=QmYourIndexerPeerID api_key=QmYourIndexerPeerID.20240101T000000Z.abc123...
```

## Next steps

- [API Reference](api-reference.md) — full endpoint documentation with request/response shapes
- [config/config.yaml](../config/config.yaml) — annotated default configuration
