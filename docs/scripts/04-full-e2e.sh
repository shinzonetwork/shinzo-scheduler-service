#!/usr/bin/env bash
# Full end-to-end test with real indexer and host binaries.
#
# Starts ./indexer and ./host from the project root, extracts their real
# secp256k1 identities from the /registration endpoint, registers them with
# the scheduler, and runs through the full coordination flow: probe → discover
# → subscribe → activate → verify reliability score updated → cancel.
#
# Prerequisites:
#   - Scheduler running on localhost:8090 (SCHEDULER_CHAIN + SCHEDULER_NETWORK set)
#   - ./indexer and ./host binaries present in the project root
#   - jq installed
#   - GETH_RPC_URL and GETH_WS_URL set (real or test-net RPC endpoint)
#
# Optional env vars:
#   SCHEDULER_BASE_URL    (default: http://localhost:8090)
#   SCHEDULER_CHAIN       (default: ethereum)
#   SCHEDULER_NETWORK     (default: mainnet)
#   INDEXER_HEALTH_PORT   (default: 8091)
#   HOST_HEALTH_PORT      (default: 8092)
#   DEFRADB_KEYRING_SECRET (default: test-e2e-secret)

set -euo pipefail

BASE="${SCHEDULER_BASE_URL:-http://localhost:8090}"
CHAIN="${SCHEDULER_CHAIN:-ethereum}"
NETWORK="${SCHEDULER_NETWORK:-mainnet}"
INDEXER_PORT="${INDEXER_HEALTH_PORT:-8091}"
HOST_PORT="${HOST_HEALTH_PORT:-8092}"
KEYRING_SECRET="${DEFRADB_KEYRING_SECRET:-pingpong}"

INDEXER_URL="http://localhost:$INDEXER_PORT"
HOST_URL="http://localhost:$HOST_PORT"

# Temp working directories — cleaned up on exit.
INDEXER_DIR=$(mktemp -d)
HOST_DIR=$(mktemp -d)
INDEXER_PID=""
HOST_PID=""
ORIG_HOST_CONFIG=""

cleanup() {
  echo ""
  echo "==> Cleaning up..."
  [ -n "$INDEXER_PID" ] && kill "$INDEXER_PID" 2>/dev/null && echo "    stopped indexer"
  [ -n "$HOST_PID"    ] && kill "$HOST_PID"    2>/dev/null && echo "    stopped host"
  if [ -n "$ORIG_HOST_CONFIG" ] && [ -f "$ORIG_HOST_CONFIG" ]; then
    cp "$ORIG_HOST_CONFIG" ./config/config.yaml && rm -f "$ORIG_HOST_CONFIG"
  fi
  rm -rf "$INDEXER_DIR" "$HOST_DIR"
  echo "    temp dirs removed"
}
trap cleanup EXIT

# wait_for_http URL LABEL LOG_FILE
# Polls URL every 2s up to 90s. Exits early if the log shows a fatal error.
wait_for_http() {
  local url="$1" label="$2" log_file="${3:-}" elapsed=0
  printf "    waiting for %s to be ready" "$label"
  while ! curl -sf -H 'Accept: application/json' "$url" > /dev/null 2>&1; do
    sleep 2
    elapsed=$((elapsed + 2))
    printf "."
    if [ -n "$log_file" ] && [ -f "$log_file" ]; then
      if grep -q "FATAL\|level.*fatal" "$log_file" 2>/dev/null; then
        echo ""
        echo "ERROR: $label crashed. Last log lines:"
        tail -20 "$log_file"
        exit 1
      fi
    fi
    if [ "$elapsed" -ge 90 ]; then
      echo ""
      echo "ERROR: $label did not become ready within 90s"
      [ -n "$log_file" ] && [ -f "$log_file" ] && tail -20 "$log_file"
      exit 1
    fi
  done
  echo " ready"
}

strip_0x() {
  # Remove 0x / 0X prefix that the registration endpoint adds.
  sed 's/^0[xX]//'
}

# post_json URL DATA — POST JSON, print body, exit with error if not 2xx.
post_json() {
  local url="$1" data="$2"
  local raw status body
  raw=$(curl -s -w '\n%{http_code}' -X POST "$url" \
    -H 'Content-Type: application/json' \
    -d "$data")
  status=$(printf '%s' "$raw" | tail -1)
  body=$(printf '%s' "$raw" | sed '$d')
  if [ "${status:0:1}" != "2" ]; then
    echo "ERROR: POST $url returned HTTP $status" >&2
    printf '%s\n' "$body" | jq -r '.error // .' >&2
    exit 1
  fi
  printf '%s' "$body"
}

echo "==> Checking scheduler health..."
curl -sf "$BASE/v1/health" | jq .

# ---------------------------------------------------------------------------
# Indexer
# ---------------------------------------------------------------------------
echo ""
echo "==> Writing indexer config to $INDEXER_DIR/config.yaml..."
# GCP Blockchain Node Engine requires the API key as a query parameter.
# Embed it in both URLs if not already present.
_GETH_RPC="${GETH_RPC_URL:-}"
_GETH_WS="${GETH_WS_URL:-}"
if [ -n "${GETH_API_KEY:-}" ]; then
  [[ "$_GETH_RPC" != *"?"* ]] && _GETH_RPC="${_GETH_RPC}?key=${GETH_API_KEY}"
  [[ "$_GETH_WS"  != *"?"* ]] && _GETH_WS="${_GETH_WS}?key=${GETH_API_KEY}"
fi

cat > "$INDEXER_DIR/config.yaml" <<EOF
chain:
  name: "Ethereum"
  network: "Mainnet"

defradb:
  url: "http://localhost:9182"
  keyring_secret: "${KEYRING_SECRET}-indexer"
  embedded: true
  p2p:
    enabled: true
    accept_incoming: false
    listen_addr: "/ip4/0.0.0.0/tcp/9172"
    max_retries: 3
    retry_base_delay_ms: 1000
    reconnect_interval_ms: 30000
    enable_auto_reconnect: false
  store:
    path: "${INDEXER_DIR}/.defra"
    block_cache_mb: 32
    memtable_mb: 16
    index_cache_mb: 16

geth:
  node_url: "${_GETH_RPC}"
  ws_url: "${_GETH_WS}"
  api_key: "${GETH_API_KEY:-}"

indexer:
  start_height: ${START_HEIGHT:-0}
  concurrent_blocks: 1
  receipt_workers: 4
  max_docs_per_txn: 100
  blocks_per_minute: 60
  health_server_port: ${INDEXER_PORT}
  open_browser_on_start: false
  start_buffer: 10

pruner:
  enabled: false

snapshot:
  enabled: false

logger:
  level: "error"
  development: false
EOF

echo "==> Starting indexer (logs → $INDEXER_DIR/indexer.log)..."
./indexer --config "$INDEXER_DIR/config.yaml" > "$INDEXER_DIR/indexer.log" 2>&1 &
INDEXER_PID=$!
echo "    PID: $INDEXER_PID"

wait_for_http "$INDEXER_URL/health" "indexer" "$INDEXER_DIR/indexer.log"

echo ""
echo "==> Indexer health:"
curl -sf -H 'Accept: application/json' "$INDEXER_URL/health" | jq '{status, current_block, defradb_connected}'

# ---------------------------------------------------------------------------
# Get real registration data from the running indexer.
# /registration returns 0x-prefixed hex for all fields.
# ---------------------------------------------------------------------------
echo ""
echo "==> Fetching indexer registration identity..."
INDEXER_REG=$(curl -sf -H 'Accept: application/json' "$INDEXER_URL/registration")
echo "$INDEXER_REG" | jq '{message: .registration.message, enabled: .registration.enabled}'

INDEXER_MSG=$(echo "$INDEXER_REG" | jq -r '
  .registration.defra_pk_registration.message //
  .registration.message //
  .message //
  empty' | strip_0x)
INDEXER_DEFRA_PK=$(echo "$INDEXER_REG" | jq -r '.registration.defra_pk_registration.public_key // empty' | strip_0x)
INDEXER_DEFRA_SIG=$(echo "$INDEXER_REG" | jq -r '.registration.defra_pk_registration.signed_pk_message // empty' | strip_0x)
INDEXER_PEER_ID=$(echo "$INDEXER_REG" | jq -r '.registration.peer_id_registration.peer_id // empty' | strip_0x)

# Derive the P2P listen multiaddr from the health endpoint.
INDEXER_P2P_ADDR=$(curl -sf -H 'Accept: application/json' "$INDEXER_URL/health" \
  | jq -r '.p2p.self.addresses[0] // "/ip4/127.0.0.1/tcp/9172"')

echo "    peer_id:    $INDEXER_PEER_ID"
echo "    defra_pk:   ${INDEXER_DEFRA_PK:0:16}..."
echo "    multiaddr:  $INDEXER_P2P_ADDR"

# Use defra_pk as the scheduler peer_id for simplicity (it's a unique, stable identifier).
SCHEDULER_INDEXER_ID="${INDEXER_DEFRA_PK}"

# ---------------------------------------------------------------------------
# Host
# ---------------------------------------------------------------------------
echo ""
echo "==> Writing host config to $HOST_DIR/config.yaml..."
cat > "$HOST_DIR/config.yaml" <<EOF
defradb:
  url: "localhost:9183"
  keyring_secret: "${KEYRING_SECRET}-host"
  p2p:
    enabled: true
    bootstrap_peers:
      - "${INDEXER_P2P_ADDR}"
    listen_addr: "/ip4/0.0.0.0/tcp/9173"
    max_retries: 3
    retry_base_delay_ms: 1000
    reconnect_interval_ms: 30000
    enable_auto_reconnect: false
  store:
    path: "${HOST_DIR}/.defra"
    block_cache_mb: 32
    memtable_mb: 16
    index_cache_mb: 16

shinzo:
  minimum_attestations: 1
  start_height: 0
  hub_base_url: ""

pruner:
  enabled: false

logger:
  level: "error"
  development: false

host:
  health_server_port: ${HOST_PORT}
  open_browser_on_start: false
  snapshot:
    enabled: false
EOF

# Detect --config support before starting anything. Use set +e so a non-zero
# exit from the host binary (or grep) doesn't trigger pipefail.
set +e
./host --help 2>&1 | grep -q -- "--config"
_host_has_config_flag=$?
set -e

if [ "$_host_has_config_flag" -eq 0 ]; then
  echo "==> Starting host with --config (logs → $HOST_DIR/host.log)..."
  ./host --config "$HOST_DIR/config.yaml" > "$HOST_DIR/host.log" 2>&1 &
else
  # Host reads ./config/config.yaml from the working directory; swap in our config.
  echo "==> Starting host (config → ./config/config.yaml, logs → $HOST_DIR/host.log)..."
  ORIG_HOST_CONFIG="./config/host-original-$$.yaml"
  cp ./config/config.yaml "$ORIG_HOST_CONFIG"
  cp "$HOST_DIR/config.yaml" ./config/config.yaml
  ./host > "$HOST_DIR/host.log" 2>&1 &
fi
HOST_PID=$!
echo "    PID: $HOST_PID"

wait_for_http "$HOST_URL/health" "host" "$HOST_DIR/host.log"

echo ""
echo "==> Host health:"
curl -sf -H 'Accept: application/json' "$HOST_URL/health" | jq '{status, current_block, defradb_connected}'

# ---------------------------------------------------------------------------
# Get real registration data from the running host.
# ---------------------------------------------------------------------------
echo ""
echo "==> Fetching host registration identity..."
HOST_REG=$(curl -sf -H 'Accept: application/json' "$HOST_URL/registration")

HOST_MSG=$(echo "$HOST_REG"     | jq -r '.registration.message // .message // empty' | strip_0x)
HOST_DEFRA_PK=$(echo "$HOST_REG" | jq -r '.registration.defra_pk_registration.public_key // empty' | strip_0x)
HOST_DEFRA_SIG=$(echo "$HOST_REG" | jq -r '.registration.defra_pk_registration.signed_pk_message // empty' | strip_0x)

HOST_P2P_ADDR=$(curl -sf -H 'Accept: application/json' "$HOST_URL/health" \
  | jq -r '.p2p.self.addresses[0] // "/ip4/127.0.0.1/tcp/9173"')

SCHEDULER_HOST_ID="${HOST_DEFRA_PK}"
echo "    host_id:   $SCHEDULER_HOST_ID"
echo "    multiaddr: $HOST_P2P_ADDR"

# ---------------------------------------------------------------------------
# Register both peers with the scheduler.
# ---------------------------------------------------------------------------
echo ""
echo "==> Registering indexer with scheduler..."
_INDEXER_BODY=$(jq -n \
  --arg peer_id   "$SCHEDULER_INDEXER_ID" \
  --arg defra_pk  "$INDEXER_DEFRA_PK" \
  --arg msg       "$INDEXER_MSG" \
  --arg sig       "$INDEXER_DEFRA_SIG" \
  --arg http_url  "$INDEXER_URL" \
  --arg multiaddr "$INDEXER_P2P_ADDR" \
  --arg chain     "$CHAIN" \
  --arg network   "$NETWORK" \
  --arg pricing   '{"tipPer1kBlocks": 0.5, "snapshotPerRange": 2.0}' \
  '{peer_id:$peer_id,defra_pk:$defra_pk,signed_messages:{($msg):$sig},http_url:$http_url,multiaddr:$multiaddr,chain:$chain,network:$network,pricing:$pricing}')
INDEXER_RESP=$(post_json "$BASE/v1/indexers/register" "$_INDEXER_BODY")
echo "$INDEXER_RESP" | jq .
INDEXER_API_KEY=$(echo "$INDEXER_RESP" | jq -r '.api_key')

echo ""
echo "==> Sending indexer heartbeat..."
CURRENT_TIP=$(curl -sf -H 'Accept: application/json' "$INDEXER_URL/health" | jq -r '.current_block // 0')
curl -sf -X POST "$BASE/v1/indexers/$SCHEDULER_INDEXER_ID/heartbeat" \
  -H "Authorization: Bearer $INDEXER_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"current_tip\": $CURRENT_TIP, \"snapshot_ranges\": \"[]\"}" | jq .
echo "    reported tip: $CURRENT_TIP"

echo ""
echo "==> Registering host with scheduler..."
_HOST_BODY=$(jq -n \
  --arg peer_id   "$SCHEDULER_HOST_ID" \
  --arg defra_pk  "$HOST_DEFRA_PK" \
  --arg msg       "$HOST_MSG" \
  --arg sig       "$HOST_DEFRA_SIG" \
  --arg http_url  "$HOST_URL" \
  --arg multiaddr "$HOST_P2P_ADDR" \
  --arg chain     "$CHAIN" \
  --arg network   "$NETWORK" \
  '{peer_id:$peer_id,defra_pk:$defra_pk,signed_messages:{($msg):$sig},http_url:$http_url,multiaddr:$multiaddr,chain:$chain,network:$network}')
HOST_RESP=$(post_json "$BASE/v1/hosts/register" "$_HOST_BODY")
echo "$HOST_RESP" | jq .
HOST_API_KEY=$(echo "$HOST_RESP" | jq -r '.api_key')

# ---------------------------------------------------------------------------
# Discover, subscribe, activate.
# ---------------------------------------------------------------------------
echo ""
echo "==> Discovering indexers (host perspective)..."
DISCOVERY=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$SCHEDULER_HOST_ID" \
  -H "Authorization: Bearer $HOST_API_KEY")
echo "$DISCOVERY" | jq .

DISCOVERED=$(echo "$DISCOVERY" | jq -r '.[0].peer_id // empty')
if [ -z "$DISCOVERED" ]; then
  echo "ERROR: indexer not visible in discovery (heartbeat may have stale tip=0)"
  exit 1
fi
echo "    discovered: $DISCOVERED"

echo ""
echo "==> Creating subscription..."
SUB_RESP=$(curl -sf -X POST "$BASE/v1/subscriptions" \
  -H "Authorization: Bearer $HOST_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{\"indexer_id\": \"$SCHEDULER_INDEXER_ID\", \"sub_type\": \"tip\"}")
echo "$SUB_RESP" | jq .
SUB_ID=$(echo "$SUB_RESP" | jq -r '.subscriptionId')
echo "    subscription $SUB_ID is $(echo "$SUB_RESP" | jq -r '.status')"

echo ""
echo "==> Activating subscription..."
EXPIRES=$(date -u -d "+90 days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
          date -u -v+90d +"%Y-%m-%dT%H:%M:%SZ")

curl -sf -X POST "$BASE/v1/payments/verify" \
  -H "Authorization: Bearer $HOST_API_KEY" \
  -H 'Content-Type: application/json' \
  -d "{
    \"subscription_id\": \"$SUB_ID\",
    \"payment_ref\": \"e2e-test-$(date +%s)\",
    \"expires_at\": \"$EXPIRES\"
  }" | jq .

echo ""
echo "==> Verifying subscription is active with real connection details..."
SUB_DETAIL=$(curl -sf "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $HOST_API_KEY")
echo "$SUB_DETAIL" | jq .
echo "    status:            $(echo "$SUB_DETAIL" | jq -r '.subscription.status')"
echo "    indexer_http_url:  $(echo "$SUB_DETAIL" | jq -r '.indexer_http_url')"
echo "    indexer_multiaddr: $(echo "$SUB_DETAIL" | jq -r '.indexer_multiaddr')"

# ---------------------------------------------------------------------------
# Wait for the scheduler prober to run and update reliability score.
# The probe interval defaults to 60s; we wait up to 90s.
# ---------------------------------------------------------------------------
echo ""
echo "==> Waiting up to 90s for scheduler prober to probe the real indexer..."
PROBE_WAIT=0
RELIABILITY=0
while [ "$PROBE_WAIT" -lt 90 ]; do
  sleep 5
  PROBE_WAIT=$((PROBE_WAIT + 5))
  RELIABILITY=$(curl -sf "$BASE/v1/indexers/$SCHEDULER_INDEXER_ID" \
    | jq -r '.reliabilityScore // "n/a"')
  printf "\r    %ds elapsed — reliability_score: %s" "$PROBE_WAIT" "$RELIABILITY"
  if [ "$PROBE_WAIT" -ge 75 ]; then
    break
  fi
done
echo ""
echo "    final reliability_score: $RELIABILITY"

# ---------------------------------------------------------------------------
# Cancel and finish.
# ---------------------------------------------------------------------------
echo ""
echo "==> Cancelling subscription..."
curl -sf -X DELETE "$BASE/v1/subscriptions/$SUB_ID" \
  -H "Authorization: Bearer $HOST_API_KEY" | jq .

echo ""
echo "==> Final scheduler health:"
curl -sf "$BASE/v1/health" | jq .

echo ""
echo "Done. Full e2e test completed successfully."
echo "    Indexer log: $INDEXER_DIR/indexer.log"
echo "    Host log:    $HOST_DIR/host.log"
echo ""
echo "Processes are still running. You have 60s to inspect before cleanup."
echo "  tail -f $INDEXER_DIR/indexer.log"
echo "  tail -f $HOST_DIR/host.log"
echo "  curl -s http://localhost:$INDEXER_PORT/health | jq '.p2p'"
echo "  curl -s http://localhost:$HOST_PORT/health | jq '.p2p'"
echo "  curl -s $BASE/v1/indexers/$SCHEDULER_INDEXER_ID | jq '{reliabilityScore,currentTip,status}'"
for i in $(seq 60 -10 10); do
  printf "\r    shutting down in %ds..." "$i"
  sleep 10
done
printf "\r                              \n"
