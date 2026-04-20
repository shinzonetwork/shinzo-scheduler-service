# API Reference

Base URL: `http://localhost:8090`

## Authentication

Authenticated endpoints require a per-request Bearer token:

```
Authorization: Bearer <peerID>.<unixTimestamp>.<signatureHex>
```

The token is a secp256k1 DER signature over `SHA256("peerID.unixTimestamp")`, using the private key corresponding to the `defra_pk` registered at sign-up. Tokens are valid for +/-60 seconds.

## Error responses

All errors return JSON with an `error` field:

```json
{"error": "description of the problem"}
```

---

## Public

### Health

```
GET /v1/health
```

Returns scheduler liveness and aggregate counts.

```bash
curl http://localhost:8090/v1/health
```

```json
{
  "status": "ok",
  "indexers": 2,
  "hosts": 1,
  "subscriptions": 3
}
```

---

### Stats

```
GET /v1/stats
```

Returns counts of active peers and subscriptions.

```bash
curl http://localhost:8090/v1/stats
```

```json
{
  "active_indexers": 2,
  "active_hosts": 1,
  "active_subscriptions": 3,
  "pending_subscriptions": 1
}
```

---

### Metrics

```
GET /v1/metrics
```

Returns aggregate reliability and probe statistics.

```bash
curl http://localhost:8090/v1/metrics
```

```json
{
  "indexers": {
    "total": 3,
    "active": 2,
    "avg_reliability": 0.94
  },
  "hosts": {
    "total": 1,
    "active": 1
  },
  "probes": {
    "total": 120,
    "successful": 115
  }
}
```

---

## Indexers

Registration requires a secp256k1 identity. Use [docs/scripts/gen-peer-identity](scripts/gen-peer-identity/) to generate test identities.

### Register

```
POST /v1/indexers/register
```

Registers a new indexer. Returns the registered peer ID and heartbeat interval.

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `peer_id` | string | yes | Unique peer identifier (typically the hex-encoded public key) |
| `defra_pk` | string | yes | Compressed secp256k1 public key, hex-encoded |
| `signed_messages` | object | yes | Map of `hex(message) → hex(DER signature)` |
| `http_url` | string | yes | HTTP base URL of the indexer |
| `multiaddr` | string | yes | libp2p multiaddr for direct connections |
| `chain` | string | yes | Chain identifier (must match scheduler config) |
| `network` | string | yes | Network identifier (must match scheduler config) |
| `pricing` | string | no | JSON: `{"tipPer1kBlocks": 0.5, "snapshotPerRange": 2.0}` |

```bash
curl -s -X POST http://localhost:8090/v1/indexers/register \
  -H 'Content-Type: application/json' \
  -d '{
    "peer_id": "03a1b2c3...",
    "defra_pk": "03a1b2c3...",
    "signed_messages": {"<msg_hex>": "<sig_hex>"},
    "http_url": "http://indexer.example.com:8080",
    "multiaddr": "/ip4/1.2.3.4/tcp/9000/p2p/03a1b2c3...",
    "chain": "ethereum",
    "network": "testnet",
    "pricing": "{\"tipPer1kBlocks\": 0.5, \"snapshotPerRange\": 2.0}"
  }'
```

```json
{
  "peer_id": "03a1b2c3...",
  "heartbeat_interval_seconds": 30
}
```

---

### Get indexer

```
GET /v1/indexers/{id}
```

Returns the indexer record.

```bash
curl http://localhost:8090/v1/indexers/03a1b2c3...
```

```json
{
  "peer_id": "03a1b2c3...",
  "http_url": "http://indexer.example.com:8080",
  "multiaddr": "/ip4/1.2.3.4/tcp/9000/p2p/03a1b2c3...",
  "chain": "ethereum",
  "network": "testnet",
  "current_tip": 19500000,
  "snapshot_ranges": "[]",
  "pricing": "{\"tipPer1kBlocks\":0.5,\"snapshotPerRange\":2.0}",
  "reliability_score": 0.97,
  "status": "active",
  "last_heartbeat": "2024-01-01T12:00:00Z",
  "registered_at": "2024-01-01T00:00:00Z"
}
```

---

### Heartbeat

```
POST /v1/indexers/{id}/heartbeat
Authorization: Bearer <token>
```

Updates the indexer's current tip and snapshot list. Must be called at least once per `heartbeat_interval_seconds` (returned at registration); if the last heartbeat is older than `staleness_window_seconds` the indexer is excluded from discovery.

**Request body:**

| Field | Type | Description |
|---|---|---|
| `current_tip` | int | Current chain head block number the indexer has ingested |
| `snapshot_ranges` | string | JSON-encoded array of snapshot descriptors; pass `"[]"` if none |

Each element of `snapshot_ranges` is an object with:

| Field | Type | Description |
|---|---|---|
| `start` | int | First block (inclusive) covered by the snapshot |
| `end` | int | Last block (inclusive) covered by the snapshot |
| `file` | string | Storage locator the host can fetch (URL, CID, or path) |
| `sizeBytes` | int | Snapshot size in bytes, used by clients for transfer budgeting |
| `createdAt` | string | RFC3339 timestamp of snapshot creation (optional) |

```bash
curl -s -X POST http://localhost:8090/v1/indexers/03a1b2c3.../heartbeat \
  -H 'Authorization: Bearer 03a1b2c3....20240101T000000Z.abc123...' \
  -H 'Content-Type: application/json' \
  -d '{
    "current_tip": 19500000,
    "snapshot_ranges": "[{\"start\":18000000,\"end\":19000000,\"file\":\"snap.tar.gz\",\"sizeBytes\":10737418240,\"createdAt\":\"2026-04-21T00:00:00Z\"}]"
  }'
```

```json
{"status": "ok"}
```

> **Indexer implementation checklist.** The indexer binary must (1) read `heartbeat_interval_seconds` from the registration response and schedule heartbeats at that cadence, (2) generate a fresh Bearer token (`peerID.unixTs.sigHex`) for every call — tokens expire after 60 seconds, (3) send `current_tip` and `snapshot_ranges` as the request body, (4) treat a non-2xx response as a failure to re-register on the next interval rather than silently retrying forever.

---

### Deregister

```
DELETE /v1/indexers/{id}
Authorization: Bearer <token>
```

Marks the indexer as inactive.

```bash
curl -s -X DELETE http://localhost:8090/v1/indexers/03a1b2c3... \
  -H 'Authorization: Bearer 03a1b2c3....20240101T000000Z.abc123...'
```

```json
{"status": "deregistered"}
```

---

## Hosts

### Register

```
POST /v1/hosts/register
```

Registers a new host. Returns the registered peer ID and heartbeat interval.

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `peer_id` | string | yes | Unique peer identifier |
| `defra_pk` | string | yes | Compressed secp256k1 public key, hex-encoded |
| `signed_messages` | object | yes | Map of `hex(message) → hex(DER signature)` |
| `http_url` | string | yes | HTTP base URL of the host |
| `multiaddr` | string | yes | libp2p multiaddr |
| `chain` | string | yes | Chain identifier |
| `network` | string | yes | Network identifier |

```bash
curl -s -X POST http://localhost:8090/v1/hosts/register \
  -H 'Content-Type: application/json' \
  -d '{
    "peer_id": "02d4e5f6...",
    "defra_pk": "02d4e5f6...",
    "signed_messages": {"<msg_hex>": "<sig_hex>"},
    "http_url": "http://host.example.com:8081",
    "multiaddr": "/ip4/5.6.7.8/tcp/9001/p2p/02d4e5f6...",
    "chain": "ethereum",
    "network": "testnet"
  }'
```

```json
{
  "peer_id": "02d4e5f6...",
  "heartbeat_interval_seconds": 30
}
```

---

### Get host

```
GET /v1/hosts/{id}
Authorization: Bearer <token>
```

Returns the host record. Only the owning host can retrieve it.

```bash
curl http://localhost:8090/v1/hosts/02d4e5f6... \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "peer_id": "02d4e5f6...",
  "http_url": "http://host.example.com:8081",
  "multiaddr": "/ip4/5.6.7.8/tcp/9001/p2p/02d4e5f6...",
  "chain": "ethereum",
  "network": "testnet",
  "status": "active",
  "last_heartbeat": "2024-01-01T12:00:00Z",
  "registered_at": "2024-01-01T00:00:00Z"
}
```

---

### Heartbeat

```
POST /v1/hosts/{id}/heartbeat
Authorization: Bearer <token>
```

Updates host liveness timestamp. Must be called at least once per `heartbeat_interval_seconds` (returned at registration). The request body is empty — hosts do not report progress, only liveness.

```bash
curl -s -X POST http://localhost:8090/v1/hosts/02d4e5f6.../heartbeat \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...' \
  -H 'Content-Type: application/json' \
  -d '{}'
```

```json
{"status": "ok"}
```

> **Host implementation checklist.** The host binary must (1) read `heartbeat_interval_seconds` from the registration response, (2) generate a fresh Bearer token per call (tokens expire after 60 seconds), (3) send an empty JSON body (`{}`) — unlike the indexer heartbeat, no fields are expected, (4) continue heartbeating for as long as it has active subscriptions, otherwise it is dropped from the discovery pool and cannot receive new subscription details.

---

### Deregister

```
DELETE /v1/hosts/{id}
Authorization: Bearer <token>
```

```bash
curl -s -X DELETE http://localhost:8090/v1/hosts/02d4e5f6... \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{"status": "deregistered"}
```

---

## Discovery

All discovery endpoints require host authentication.

### Find indexers (tip session)

```
GET /v1/discover/indexers
Authorization: Bearer <token>
```

Returns ranked indexers eligible for tip sessions.

**Query parameters:**

| Param | Required | Description |
|---|---|---|
| `chain` | yes | Chain identifier |
| `network` | yes | Network identifier |
| `min_reliability` | no | Minimum reliability score (0.0–1.0) |
| `limit` | no | Maximum results (default: all) |
| `host_id` | no | Caller's peer ID (used for diversity weighting) |
| `max_tip_per_1k` | no | Budget ceiling: tokens per 1k blocks |
| `max_snapshot_per_range` | no | Budget ceiling: tokens per snapshot range |

```bash
curl -s "http://localhost:8090/v1/discover/indexers?chain=ethereum&network=testnet&limit=5&host_id=02d4e5f6..." \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
[
  {
    "peer_id": "03a1b2c3...",
    "http_url": "http://indexer.example.com:8080",
    "multiaddr": "/ip4/1.2.3.4/tcp/9000/p2p/03a1b2c3...",
    "reliability_score": 0.97,
    "current_tip": 19500000,
    "pricing": {"tipPer1kBlocks": 0.5, "snapshotPerRange": 2.0},
    "snapshot_creation_required": false
  }
]
```

---

### Find indexers (snapshot session)

```
GET /v1/discover/snapshots
Authorization: Bearer <token>
```

Returns ranked indexers that hold or can create a snapshot for the requested block range.

**Query parameters:**

| Param | Required | Description |
|---|---|---|
| `chain` | yes | Chain identifier |
| `network` | yes | Network identifier |
| `block_from` | yes | Start block of the requested range |
| `block_to` | yes | End block of the requested range |
| `limit` | no | Maximum results |
| `host_id` | no | Caller's peer ID |
| `max_tip_per_1k` | no | Budget ceiling |
| `max_snapshot_per_range` | no | Budget ceiling |

```bash
curl -s "http://localhost:8090/v1/discover/snapshots?chain=ethereum&network=testnet&block_from=18000000&block_to=19000000" \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
[
  {
    "peer_id": "03a1b2c3...",
    "reliability_score": 0.97,
    "pricing": {"tipPer1kBlocks": 0.5, "snapshotPerRange": 2.0},
    "snapshot_creation_required": false
  },
  {
    "peer_id": "03c3d4e5...",
    "reliability_score": 0.91,
    "pricing": {"tipPer1kBlocks": 0.4, "snapshotPerRange": 1.5},
    "snapshot_creation_required": true
  }
]
```

---

### Unified match

```
GET /v1/discover/match
Authorization: Bearer <token>
```

Routes to snapshot discovery if `block_from` and `block_to` are present; otherwise routes to tip discovery. Accepts the same parameters as both discovery endpoints.

```bash
# Tip session
curl -s "http://localhost:8090/v1/discover/match?chain=ethereum&network=testnet" \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'

# Snapshot session
curl -s "http://localhost:8090/v1/discover/match?chain=ethereum&network=testnet&block_from=18000000&block_to=19000000" \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

---

## Subscriptions

### Create

```
POST /v1/subscriptions
Authorization: Bearer <token>
```

Creates a subscription in `pending` state. The subscription must be activated via `/v1/payments/verify` before the indexer's multiaddr is returned.

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `indexer_id` | string | yes | Target indexer's peer ID |
| `sub_type` | string | yes | `"tip"` or `"snapshot"` |
| `block_from` | int | snapshot only | Start block |
| `block_to` | int | snapshot only | End block |
| `expires_at` | string | no | RFC3339 expiry (set by payment handler on activation) |

```bash
curl -s -X POST http://localhost:8090/v1/subscriptions \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...' \
  -H 'Content-Type: application/json' \
  -d '{
    "indexer_id": "03a1b2c3...",
    "sub_type": "tip"
  }'
```

```json
{
  "_docID": "bae-abc123...",
  "hostId": "02d4e5f6...",
  "indexerId": "03a1b2c3...",
  "subType": "tip",
  "status": "pending",
  "createdAt": "2024-01-01T12:00:00Z"
}
```

---

### List

```
GET /v1/subscriptions
Authorization: Bearer <token>
```

Lists subscriptions belonging to the authenticated host.

```bash
curl -s http://localhost:8090/v1/subscriptions \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
[
  {
    "_docID": "bae-abc123...",
    "hostId": "02d4e5f6...",
    "indexerId": "03a1b2c3...",
    "subType": "tip",
    "status": "active",
    "paymentRef": "tx-ref-001",
    "expiresAt": "2024-04-01T00:00:00Z",
    "createdAt": "2024-01-01T12:00:00Z"
  }
]
```

---

### Get

```
GET /v1/subscriptions/{id}
Authorization: Bearer <token>
```

Returns a single subscription. When status is `active`, the response includes `indexer_multiaddr` and `indexer_http_url`.

```bash
curl -s http://localhost:8090/v1/subscriptions/7ea4091a-3523-4684-bb14-d874e03b9e7e \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "subscription": {
    "_docID": "bae-abc123...",
    "hostId": "02d4e5f6...",
    "indexerId": "03a1b2c3...",
    "subType": "tip",
    "status": "active",
    "paymentRef": "tx-ref-001",
    "expiresAt": "2024-04-01T00:00:00Z"
  },
  "indexer_multiaddr": "/ip4/1.2.3.4/tcp/9000/p2p/03a1b2c3...",
  "indexer_http_url": "http://indexer.example.com:8080"
}
```

---

### Cancel

```
DELETE /v1/subscriptions/{id}
Authorization: Bearer <token>
```

Cancels a subscription (only the owning host can cancel).

```bash
curl -s -X DELETE http://localhost:8090/v1/subscriptions/7ea4091a-3523-4684-bb14-d874e03b9e7e \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{"status": "cancelled"}
```

---

## Payments

### Get quote

```
GET /v1/quotes
Authorization: Bearer <token>
```

Calculates the price for a session based on the indexer's pricing configuration.

**Query parameters:**

| Param | Required | Description |
|---|---|---|
| `indexer_id` | yes | Target indexer's peer ID |
| `type` | yes | `"tip"` or `"snapshot"` |
| `blocks` | tip only | Number of blocks (default 1000) |
| `block_from` | snapshot only | Start block |
| `block_to` | snapshot only | End block |

```bash
curl -s "http://localhost:8090/v1/quotes?indexer_id=03a1b2c3...&type=tip&blocks=5000" \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "indexer_id": "03a1b2c3...",
  "sub_type": "tip",
  "block_from": 0,
  "block_to": 0,
  "price_tokens": 2.5,
  "currency": "SHINZO",
  "valid_until": "2024-01-01T12:15:00Z"
}
```

---

### Verify payment

```
POST /v1/payments/verify
Authorization: Bearer <token>
```

Activates a pending subscription. With ShinzoHub enabled, a `tx_hash` triggers on-chain verification against Tendermint RPC before activation. Without it, `payment_ref` is stored as a trust-based reference.

**Request body:**

| Field | Type | Required | Description |
|---|---|---|---|
| `subscription_id` | string | yes | Subscription doc ID to activate |
| `payment_ref` | string | yes | Payment reference (tx hash, invoice ID, etc.) |
| `tx_hash` | string | no | Tendermint tx hash for on-chain verification |
| `expires_at` | string | no | RFC3339 expiry for the subscription |

```bash
curl -s -X POST http://localhost:8090/v1/payments/verify \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...' \
  -H 'Content-Type: application/json' \
  -d '{
    "subscription_id": "bae-abc123...",
    "payment_ref": "invoice-2024-001",
    "expires_at": "2024-04-01T00:00:00Z"
  }'
```

```json
{"status": "activated"}
```

---

## Accounting

Accounting endpoints are only available when `scheduler.accounting.enabled: true`.

### Submit delivery claim

```
POST /v1/claims
Authorization: Bearer <token>
```

Indexer asserts it delivered a specific block (identified by CID) to a session. Duplicate claims for the same (session, block) pair with different CIDs are rejected as fraud.

**Request body:**

| Field | Type | Description |
|---|---|---|
| `session_id` | string | Subscription doc ID |
| `indexer_id` | string | Claiming indexer's peer ID |
| `block_number` | int | Block number delivered |
| `cid` | string | Content identifier of the delivered block |

```bash
curl -s -X POST http://localhost:8090/v1/claims \
  -H 'Authorization: Bearer 03a1b2c3....20240101T000000Z.abc123...' \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "bae-abc123...",
    "indexer_id": "03a1b2c3...",
    "block_number": 19500001,
    "cid": "bafy..."
  }'
```

```json
{
  "doc_id": "bae-claim-001...",
  "session_id": "bae-abc123...",
  "indexer_id": "03a1b2c3...",
  "block_number": 19500001,
  "cid": "bafy...",
  "created_at": "2024-01-01T12:01:00Z"
}
```

---

### Submit attestation

```
POST /v1/attestations
Authorization: Bearer <token>
```

Host confirms receipt of a block for a session. Attestations are append-only.

**Request body:**

| Field | Type | Description |
|---|---|---|
| `session_id` | string | Subscription doc ID |
| `host_id` | string | Attesting host's peer ID |
| `block_number` | int | Block number received |
| `cid` | string | Content identifier as received |

```bash
curl -s -X POST http://localhost:8090/v1/attestations \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...' \
  -H 'Content-Type: application/json' \
  -d '{
    "session_id": "bae-abc123...",
    "host_id": "02d4e5f6...",
    "block_number": 19500001,
    "cid": "bafy..."
  }'
```

```json
{
  "doc_id": "bae-attest-001...",
  "session_id": "bae-abc123...",
  "host_id": "02d4e5f6...",
  "block_number": 19500001,
  "cid": "bafy...",
  "created_at": "2024-01-01T12:01:30Z"
}
```

---

### Session ledger

```
GET /v1/sessions/{id}/ledger
Authorization: Bearer <token>
```

Returns the verified block count and remaining credit for a session.

```bash
curl -s http://localhost:8090/v1/sessions/bae-abc123.../ledger \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "session_id": "bae-abc123...",
  "blocks_verified": 1250,
  "credit_remaining": 37.5
}
```

---

### Session comparisons

```
GET /v1/sessions/{id}/comparisons
Authorization: Bearer <token>
```

Returns all claim-vs-attestation comparison outcomes for a session.

Possible outcomes: `clean_delivery`, `under_report`, `mismatch`, `indexer_silent`, `host_silent`.

```bash
curl -s http://localhost:8090/v1/sessions/bae-abc123.../comparisons \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
[
  {
    "session_id": "bae-abc123...",
    "block_number": 19500001,
    "outcome": "clean_delivery",
    "claim_cid": "bafy...",
    "attestation_cid": "bafy...",
    "compared_at": "2024-01-01T12:02:00Z"
  }
]
```

---

## Settlement

Settlement endpoints are only available when `scheduler.settlement.enabled: true`.

### Escrow account

```
GET /v1/escrow/{session_id}
Authorization: Bearer <token>
```

Returns the escrow balance and drain state for a session.

```bash
curl -s http://localhost:8090/v1/escrow/bae-abc123... \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "session_id": "bae-abc123...",
  "balance": 100.0,
  "drained": 12.5,
  "low_credit_signalled": false
}
```

---

### Settlement record

```
GET /v1/settlements/{session_id}
Authorization: Bearer <token>
```

Returns the batch settlement record for a session once finalized.

```bash
curl -s http://localhost:8090/v1/settlements/bae-abc123... \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "session_id": "bae-abc123...",
  "blocks_settled": 1250,
  "amount_paid": 37.5,
  "amount_returned": 62.5,
  "settled_at": "2024-01-01T14:00:00Z"
}
```

---

### Verdict

```
GET /v1/verdicts/{session_id}
Authorization: Bearer <token>
```

Returns the M-of-N signed verdict document for a session, if one has been produced.

```bash
curl -s http://localhost:8090/v1/verdicts/bae-abc123... \
  -H 'Authorization: Bearer 02d4e5f6....20240101T000000Z.xyz789...'
```

```json
{
  "session_id": "bae-abc123...",
  "outcome": "clean_settlement",
  "signatures": ["<sig1_hex>"],
  "threshold_m": 1,
  "threshold_n": 1,
  "signed_at": "2024-01-01T14:00:00Z"
}
```
