#!/usr/bin/env bash
# Happy path: 1 indexer + 2 hosts
#
# Demonstrates: a single indexer serving independent subscriptions to two
# separate hosts simultaneously. Each host discovers the indexer, subscribes,
# and activates independently. Both subscriptions remain active at the same time.
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

# -- Register indexer ---------------------------------------------------------
echo ""
echo "==> Generating indexer identity..."
IDX_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
IDX_PEER_ID=$(echo "$IDX_IDENTITY" | jq -r '.peer_id')
echo "    peer_id: $IDX_PEER_ID"

IDX_REG=$(echo "$IDX_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://indexer.local:8080",
    multiaddr: "/ip4/127.0.0.1/tcp/9030",
    chain: $chain,
    network: $network,
    pricing: "{\"tipPer1kBlocks\": 0.5, \"snapshotPerRange\": 2.0}"
  }')

IDX_RESP=$(curl -sf -X POST "$BASE/v1/indexers/register" \
  -H 'Content-Type: application/json' \
  -d "$IDX_REG")
IDX_API_KEY=$(echo "$IDX_RESP" | jq -r '.api_key')
echo "    registered"

echo "==> Sending indexer heartbeat..."
curl -sf -X POST "$BASE/v1/indexers/$IDX_PEER_ID/heartbeat" \
  -H "Authorization: Bearer $IDX_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"current_tip": 19500000, "snapshot_ranges": "[]"}' > /dev/null
echo "    heartbeat sent (tip=19500000)"

# -- Register host 1 ----------------------------------------------------------
echo ""
echo "==> Generating host-1 identity..."
H1_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
H1_PEER_ID=$(echo "$H1_IDENTITY" | jq -r '.peer_id')
echo "    peer_id: $H1_PEER_ID"

H1_REG=$(echo "$H1_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://host1.local:8081",
    multiaddr: "/ip4/127.0.0.1/tcp/9031",
    chain: $chain,
    network: $network
  }')

H1_RESP=$(curl -sf -X POST "$BASE/v1/hosts/register" \
  -H 'Content-Type: application/json' \
  -d "$H1_REG")
H1_API_KEY=$(echo "$H1_RESP" | jq -r '.api_key')
echo "    registered"

# -- Register host 2 ----------------------------------------------------------
echo ""
echo "==> Generating host-2 identity..."
H2_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
H2_PEER_ID=$(echo "$H2_IDENTITY" | jq -r '.peer_id')
echo "    peer_id: $H2_PEER_ID"

H2_REG=$(echo "$H2_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://host2.local:8082",
    multiaddr: "/ip4/127.0.0.1/tcp/9032",
    chain: $chain,
    network: $network
  }')

H2_RESP=$(curl -sf -X POST "$BASE/v1/hosts/register" \
  -H 'Content-Type: application/json' \
  -d "$H2_REG")
H2_API_KEY=$(echo "$H2_RESP" | jq -r '.api_key')
echo "    registered"

EXPIRES=$(date -u -d "+90 days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
          date -u -v+90d +"%Y-%m-%dT%H:%M:%SZ")

# -- Host 1: discover, subscribe, activate ------------------------------------
echo ""
echo "==> host-1: discovering indexers..."
H1_DISC=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$H1_PEER_ID" \
  -H "Authorization: Bearer $H1_API_KEY")
echo "$H1_DISC" | jq '.[0] | {peer_id, reliability_score}'

echo ""
echo "==> host-1: creating subscription..."
H1_SUB_RESP=$(curl -sf -X POST "$BASE/v1/subscriptions" \
  -H "Authorization: Bearer $H1_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"indexer_id\": \"$IDX_PEER_ID\", \"sub_type\": \"tip\"}")
H1_SUB_ID=$(echo "$H1_SUB_RESP" | jq -r '.subscriptionId')
echo "    subscription $H1_SUB_ID created (status: $(echo "$H1_SUB_RESP" | jq -r '.status'))"

echo ""
echo "==> host-1: activating subscription..."
curl -sf -X POST "$BASE/v1/payments/verify" \
  -H "Authorization: Bearer $H1_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"subscription_id\": \"$H1_SUB_ID\",
    \"payment_ref\": \"h1-payment-$(date +%s)\",
    \"expires_at\": \"$EXPIRES\"
  }" | jq .

# -- Host 2: discover, subscribe, activate ------------------------------------
echo ""
echo "==> host-2: discovering indexers..."
H2_DISC=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$H2_PEER_ID" \
  -H "Authorization: Bearer $H2_API_KEY")
echo "$H2_DISC" | jq '.[0] | {peer_id, reliability_score}'

echo ""
echo "==> host-2: creating subscription..."
H2_SUB_RESP=$(curl -sf -X POST "$BASE/v1/subscriptions" \
  -H "Authorization: Bearer $H2_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"indexer_id\": \"$IDX_PEER_ID\", \"sub_type\": \"tip\"}")
H2_SUB_ID=$(echo "$H2_SUB_RESP" | jq -r '.subscriptionId')
echo "    subscription $H2_SUB_ID created (status: $(echo "$H2_SUB_RESP" | jq -r '.status'))"

echo ""
echo "==> host-2: activating subscription..."
curl -sf -X POST "$BASE/v1/payments/verify" \
  -H "Authorization: Bearer $H2_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"subscription_id\": \"$H2_SUB_ID\",
    \"payment_ref\": \"h2-payment-$(date +%s)\",
    \"expires_at\": \"$EXPIRES\"
  }" | jq .

# -- Verify both subscriptions are active simultaneously ----------------------
echo ""
echo "==> Verifying both subscriptions are independently active..."

H1_STATUS=$(curl -sf "$BASE/v1/subscriptions/$H1_SUB_ID" \
  -H "Authorization: Bearer $H1_API_KEY" | jq -r '.subscription.status')
H2_STATUS=$(curl -sf "$BASE/v1/subscriptions/$H2_SUB_ID" \
  -H "Authorization: Bearer $H2_API_KEY" | jq -r '.subscription.status')

echo "    host-1 subscription $H1_SUB_ID: $H1_STATUS"
echo "    host-2 subscription $H2_SUB_ID: $H2_STATUS"

if [ "$H1_SUB_ID" = "$H2_SUB_ID" ]; then
  echo "ERROR: both hosts received the same subscription ID"
  exit 1
fi
echo "    confirmed: separate session IDs"

# -- Host 1: heartbeat --------------------------------------------------------
echo ""
echo "==> host-1: sending heartbeat..."
curl -sf -X POST "$BASE/v1/hosts/$H1_PEER_ID/heartbeat" \
  -H "Authorization: Bearer $H1_API_KEY" \
  -H 'Content-Type: application/json' \
  -d '{}' | jq .

# -- Host 2: cancel -----------------------------------------------------------
echo ""
echo "==> host-2: cancelling subscription..."
curl -sf -X DELETE "$BASE/v1/subscriptions/$H2_SUB_ID" \
  -H "Authorization: Bearer $H2_API_KEY" | jq .

echo ""
echo "==> host-1 subscription still active after host-2 cancels..."
H1_FINAL=$(curl -sf "$BASE/v1/subscriptions/$H1_SUB_ID" \
  -H "Authorization: Bearer $H1_API_KEY")
echo "    status: $(echo "$H1_FINAL" | jq -r '.subscription.status')"

echo ""
echo "==> Cleaning up: cancelling host-1 subscription..."
curl -sf -X DELETE "$BASE/v1/subscriptions/$H1_SUB_ID" \
  -H "Authorization: Bearer $H1_API_KEY" | jq .

echo ""
echo "Done. Happy path 03-two-hosts completed successfully."
