# Manual Test Scripts

Shell scripts for testing common scheduler topologies against a running instance.

## Prerequisites

```bash
# Scheduler running (see docs/getting-started.md)
SCHEDULER_CHAIN=ethereum SCHEDULER_NETWORK=testnet \
  go run ./cmd/scheduler --config config/config.yaml

# jq for JSON parsing
brew install jq   # macOS
apt install jq    # Debian/Ubuntu
```

Set environment variables to match your running scheduler:

```bash
export SCHEDULER_CHAIN=ethereum
export SCHEDULER_NETWORK=testnet
export SCHEDULER_BASE_URL=http://localhost:8090  # optional, default
```

## Peer identity generation

Registration requires secp256k1 signatures. The `gen-peer-identity` tool generates a fresh key pair and constructs the registration payload automatically:

```bash
go run ./docs/scripts/gen-peer-identity
```

Output:

```json
{
  "peer_id": "03a1b2...",
  "defra_pk": "03a1b2...",
  "signed_messages": {
    "<msg_hex>": "<sig_hex>"
  }
}
```

The scripts call this tool internally — no manual key management required.

## Scripts

### 01-basic.sh — 1 indexer + 1 host

The core happy path: register one indexer and one host, discover, get a quote, create and activate a subscription, then cancel it.

```bash
bash docs/scripts/01-basic.sh
```

### 02-two-indexers.sh — 2 indexers + 1 host

Register two indexers with different pricing, run discovery, and subscribe to the top-ranked result. Demonstrates the ranking and selection logic.

```bash
bash docs/scripts/02-two-indexers.sh
```

### 03-two-hosts.sh — 1 indexer + 2 hosts

Register one indexer and two hosts. Each host independently discovers, subscribes, and activates. Both subscriptions are active concurrently with separate session IDs.

```bash
bash docs/scripts/03-two-hosts.sh
```

### 04-full-e2e.sh — real binaries (1 indexer + 1 host)

Starts the real `./indexer` and `./host` binaries from the project root. Extracts their actual secp256k1 identities from the `/registration` endpoint, registers them with the scheduler, and runs the full coordination flow including waiting for the scheduler prober to hit the live indexer health endpoint and update the reliability score.

```bash
# Requires real Ethereum RPC endpoints
GETH_RPC_URL=https://... \
GETH_WS_URL=wss://... \
GETH_API_KEY=your_api_key \
START_HEIGHT=23700000 \
SCHEDULER_CHAIN=ethereum SCHEDULER_NETWORK=mainnet \
  bash docs/scripts/04-full-e2e.sh
```

For GCP Blockchain Node Engine the script automatically appends `?key=<GETH_API_KEY>` to both URLs if they do not already contain a query string.

Unlike scripts 01–03 which simulate via API calls alone, this script exercises real process lifecycle, real `/health` probing, and the live P2P bootstrap between the indexer and host DefraDB nodes.
