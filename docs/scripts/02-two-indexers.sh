#!/usr/bin/env bash
# Happy path: 2 indexers + 1 host
#
# Demonstrates: parallel indexer registration, discovery ranking by reliability
# and pricing, subscription to the best candidate.
#
# Prerequisites:
#   - Scheduler running on localhost:8090
#   - jq installed
#   - SCHEDULER_CHAIN and SCHEDULER_NETWORK match the running scheduler

set -euo pipefail

BASE="${SCHEDULER_BASE_URL:-http://localhost:8090}"
CHAIN="${SCHEDULER_CHAIN:-ethereum}"
NETWORK="${SCHEDULER_NETWORK:-testnet}"

echo "==> Checking scheduler health..."
curl -sf "$BASE/v1/health" | jq .

# -- Register indexer 1 -------------------------------------------------------
echo ""
echo "==> Generating identity for indexer-1 (pricing: 0.5 per 1k blocks)..."
IDX1_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
IDX1_PEER_ID=$(echo "$IDX1_IDENTITY" | jq -r '.peer_id')
echo "    peer_id: $IDX1_PEER_ID"

IDX1_REG=$(echo "$IDX1_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://indexer1.local:8080",
    multiaddr: "/ip4/127.0.0.1/tcp/9010",
    chain: $chain,
    network: $network,
    pricing: "{\"tipPer1kBlocks\": 0.5, \"snapshotPerRange\": 2.0}"
  }')

IDX1_RESP=$(curl -sf -X POST "$BASE/v1/indexers/register" \
  -H 'Content-Type: application/json' \
  -d "$IDX1_REG")
IDX1_API_KEY=$(echo "$IDX1_RESP" | jq -r '.api_key')
echo "    registered: $(echo "$IDX1_RESP" | jq -r '.peer_id')"

curl -sf -X POST "$BASE/v1/indexers/$IDX1_PEER_ID/heartbeat" \
  -H "Authorization: Bearer $IDX1_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"current_tip": 19500000, "snapshot_ranges": "[]"}' > /dev/null
echo "    heartbeat sent (tip=19500000)"

# -- Register indexer 2 -------------------------------------------------------
echo ""
echo "==> Generating identity for indexer-2 (pricing: 0.3 per 1k blocks, cheaper)..."
IDX2_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
IDX2_PEER_ID=$(echo "$IDX2_IDENTITY" | jq -r '.peer_id')
echo "    peer_id: $IDX2_PEER_ID"

IDX2_REG=$(echo "$IDX2_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://indexer2.local:8080",
    multiaddr: "/ip4/127.0.0.1/tcp/9011",
    chain: $chain,
    network: $network,
    pricing: "{\"tipPer1kBlocks\": 0.3, \"snapshotPerRange\": 1.5}"
  }')

IDX2_RESP=$(curl -sf -X POST "$BASE/v1/indexers/register" \
  -H 'Content-Type: application/json' \
  -d "$IDX2_REG")
IDX2_API_KEY=$(echo "$IDX2_RESP" | jq -r '.api_key')
echo "    registered: $(echo "$IDX2_RESP" | jq -r '.peer_id')"

curl -sf -X POST "$BASE/v1/indexers/$IDX2_PEER_ID/heartbeat" \
  -H "Authorization: Bearer $IDX2_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"current_tip": 19500000, "snapshot_ranges": "[]"}' > /dev/null
echo "    heartbeat sent (tip=19500000)"

# -- Register host ------------------------------------------------------------
echo ""
echo "==> Generating host identity..."
HOST_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
HOST_PEER_ID=$(echo "$HOST_IDENTITY" | jq -r '.peer_id')

HOST_REG=$(echo "$HOST_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://host.local:8081",
    multiaddr: "/ip4/127.0.0.1/tcp/9020",
    chain: $chain,
    network: $network
  }')

HOST_RESP=$(curl -sf -X POST "$BASE/v1/hosts/register" \
  -H 'Content-Type: application/json' \
  -d "$HOST_REG")
HOST_API_KEY=$(echo "$HOST_RESP" | jq -r '.api_key')
echo "    registered host: $HOST_PEER_ID"

# -- Discovery: expect both indexers ------------------------------------------
echo ""
echo "==> Discovering indexers (expect both to appear)..."
DISCOVERY=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$HOST_PEER_ID" \
  -H "Authorization: Bearer $HOST_API_KEY")
echo "$DISCOVERY" | jq .

COUNT=$(echo "$DISCOVERY" | jq 'length')
echo "    found $COUNT indexer(s)"

if [ "$COUNT" -lt 2 ]; then
  echo "WARNING: expected at least 2 indexers in discovery result"
fi

# Pick the first result (scheduler ranks by reliability and diversity)
BEST_INDEXER=$(echo "$DISCOVERY" | jq -r '.[0].peer_id // empty')
if [ -z "$BEST_INDEXER" ]; then
  echo "ERROR: no indexers discovered"
  exit 1
fi
echo "    subscribing to: $BEST_INDEXER"

# -- Subscribe to the top-ranked indexer -------------------------------------
echo ""
echo "==> Getting price quote for top-ranked indexer..."
curl -sf \
  "$BASE/v1/quotes?indexer_id=$BEST_INDEXER&type=tip&blocks=10000" \
  -H "Authorization: Bearer $HOST_API_KEY" | jq .

echo ""
echo "==> Creating subscription..."
SUB_RESP=$(curl -sf -X POST "$BASE/v1/subscriptions" \
  -H "Authorization: Bearer $HOST_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"indexer_id\": \"$BEST_INDEXER\", \"sub_type\": \"tip\"}")
echo "$SUB_RESP" | jq .
SUB_ID=$(echo "$SUB_RESP" | jq -r '.subscriptionId')

echo ""
echo "==> Activating subscription..."
EXPIRES=$(date -u -d "+90 days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
          date -u -v+90d +"%Y-%m-%dT%H:%M:%SZ")

curl -sf -X POST "$BASE/v1/payments/verify" \
  -H "Authorization: Bearer $HOST_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"subscription_id\": \"$SUB_ID\",
    \"payment_ref\": \"manual-test-$(date +%s)\",
    \"expires_at\": \"$EXPIRES\"
  }" | jq .

echo ""
echo "==> Verifying active subscription returns indexer connection details..."
SUB_DETAIL=$(curl -sf "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $HOST_API_KEY")
echo "$SUB_DETAIL" | jq .
echo "    status: $(echo "$SUB_DETAIL" | jq -r '.subscription.status')"
echo "    multiaddr: $(echo "$SUB_DETAIL" | jq -r '.indexer_multiaddr // "not set"')"

echo ""
echo "==> Cancelling subscription..."
curl -sf -X DELETE "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $HOST_API_KEY" | jq .

echo ""
echo "Done. Happy path 02-two-indexers completed successfully."
