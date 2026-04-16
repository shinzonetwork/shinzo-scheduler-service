#!/usr/bin/env bash
# Happy path: 1 indexer + 1 host
#
# Demonstrates: registration, discovery, subscription creation,
# payment activation, and cancellation.
#
# Prerequisites:
#   - Scheduler running on localhost:8090
#   - jq installed
#   - SCHEDULER_CHAIN and SCHEDULER_NETWORK match the running scheduler

set -euo pipefail

BASE="${SCHEDULER_BASE_URL:-http://localhost:8090}"
CHAIN="${SCHEDULER_CHAIN:-ethereum}"
NETWORK="${SCHEDULER_NETWORK:-testnet}"

# gen_token PRIVATE_KEY PEER_ID — generate a per-request auth token.
gen_token() {
  go run ./docs/scripts/gen-auth-token --private-key "$1" --peer-id "$2"
}

echo "==> Checking scheduler health..."
curl -sf "$BASE/v1/health" | jq .

echo ""
echo "==> Generating indexer identity..."
INDEXER_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
INDEXER_PEER_ID=$(echo "$INDEXER_IDENTITY" | jq -r '.peer_id')
INDEXER_PRIV=$(echo "$INDEXER_IDENTITY" | jq -r '.private_key')
echo "    peer_id: $INDEXER_PEER_ID"

echo ""
echo "==> Registering indexer..."
INDEXER_REG=$(echo "$INDEXER_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://indexer.local:8080",
    multiaddr: "/ip4/127.0.0.1/tcp/9000",
    chain: $chain,
    network: $network,
    pricing: "{\"tipPer1kBlocks\": 0.5, \"snapshotPerRange\": 2.0}"
  }')

INDEXER_RESP=$(curl -sf -X POST "$BASE/v1/indexers/register" \
  -H 'Content-Type: application/json' \
  -d "$INDEXER_REG")

echo "$INDEXER_RESP" | jq .

echo ""
echo "==> Sending indexer heartbeat (tip=19500000)..."
curl -sf -X POST "$BASE/v1/indexers/$INDEXER_PEER_ID/heartbeat" \
  -H "Authorization: Bearer $(gen_token "$INDEXER_PRIV" "$INDEXER_PEER_ID")" \
  -H 'Content-Type: application/json' \
  -d '{"current_tip": 19500000, "snapshot_ranges": "[]"}' | jq .

echo ""
echo "==> Generating host identity..."
HOST_IDENTITY=$(go run ./docs/scripts/gen-peer-identity)
HOST_PEER_ID=$(echo "$HOST_IDENTITY" | jq -r '.peer_id')
HOST_PRIV=$(echo "$HOST_IDENTITY" | jq -r '.private_key')
echo "    peer_id: $HOST_PEER_ID"

echo ""
echo "==> Registering host..."
HOST_REG=$(echo "$HOST_IDENTITY" | jq \
  --arg chain "$CHAIN" \
  --arg network "$NETWORK" \
  '. + {
    http_url: "http://host.local:8081",
    multiaddr: "/ip4/127.0.0.1/tcp/9001",
    chain: $chain,
    network: $network
  }')

HOST_RESP=$(curl -sf -X POST "$BASE/v1/hosts/register" \
  -H 'Content-Type: application/json' \
  -d "$HOST_REG")

echo "$HOST_RESP" | jq .

echo ""
echo "==> Discovering indexers (host perspective)..."
DISCOVERY=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$HOST_PEER_ID" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")")
echo "$DISCOVERY" | jq .

DISCOVERED_INDEXER=$(echo "$DISCOVERY" | jq -r '.[0].peer_id // empty')
if [ -z "$DISCOVERED_INDEXER" ]; then
  echo "ERROR: no indexers discovered (heartbeat staleness window may not have passed yet)"
  exit 1
fi

echo ""
echo "==> Getting a price quote..."
curl -sf \
  "$BASE/v1/quotes?indexer_id=$INDEXER_PEER_ID&type=tip&blocks=5000" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")" | jq .

echo ""
echo "==> Creating subscription..."
SUB_RESP=$(curl -sf -X POST "$BASE/v1/subscriptions" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")" \
  -H 'Content-Type: application/json' \
  -d "{\"indexer_id\": \"$INDEXER_PEER_ID\", \"sub_type\": \"tip\"}")

echo "$SUB_RESP" | jq .
SUB_ID=$(echo "$SUB_RESP" | jq -r '.subscriptionId')
SUB_STATUS=$(echo "$SUB_RESP" | jq -r '.status')
echo "    subscription $SUB_ID is $SUB_STATUS"

echo ""
echo "==> Verifying payment (trust-based, no on-chain tx)..."
EXPIRES=$(date -u -d "+90 days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
          date -u -v+90d +"%Y-%m-%dT%H:%M:%SZ")

curl -sf -X POST "$BASE/v1/payments/verify" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")" \
  -H 'Content-Type: application/json' \
  -d "{
    \"subscription_id\": \"$SUB_ID\",
    \"payment_ref\": \"manual-test-$(date +%s)\",
    \"expires_at\": \"$EXPIRES\"
  }" | jq .

echo ""
echo "==> Getting subscription (expect active + indexer multiaddr)..."
SUB_DETAIL=$(curl -sf "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")")
echo "$SUB_DETAIL" | jq .
echo "    status: $(echo "$SUB_DETAIL" | jq -r '.subscription.status')"

echo ""
echo "==> Cancelling subscription..."
curl -sf -X DELETE "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $(gen_token "$HOST_PRIV" "$HOST_PEER_ID")" | jq .

echo ""
echo "==> Final health check..."
curl -sf "$BASE/v1/health" | jq .

echo ""
echo "Done. Happy path 01-basic completed successfully."
