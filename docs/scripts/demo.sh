#!/usr/bin/env bash
# demo.sh — runs scheduler + N indexers + 1 host locally.
# Steps: start processes, register, discover, subscribe, activate, teardown.
#
# Requires: ./scheduler ./indexer ./host binaries, jq, GETH_RPC_URL, GETH_WS_URL.
# Optional: SCHEDULER_CHAIN, SCHEDULER_NETWORK, SCHEDULER_HMAC_SECRET,
#           DEFRADB_KEYRING_SECRET, GETH_API_KEY.

set -euo pipefail

BASE="http://localhost:8090"
KEYRING_SECRET="${DEFRADB_KEYRING_SECRET:-pingpong}"

# ---------------------------------------------------------------------------
# Process state — initialised before the cleanup trap so it always sees them.
# ---------------------------------------------------------------------------
SCHEDULER_PID=""
SCHEDULER_DIR=""
INDEXER_PIDS=()
INDEXER_DIRS=()
INDEXER_PORTS=()
INDEXER_IDS=()
INDEXER_KEYS=()
HOST_PID=""
HOST_DIR=""
ORIG_HOST_CONFIG=""
HOST_KEY=""
HOST_ID=""
SUB_IDS=()
CHAIN=""
NETWORK=""

cleanup() {
  echo "" >&2
  [ -n "$SCHEDULER_PID" ] && kill "$SCHEDULER_PID" 2>/dev/null || true
  local pid
  for pid in "${INDEXER_PIDS[@]+"${INDEXER_PIDS[@]}"}"; do
    kill "$pid" 2>/dev/null || true
  done
  [ -n "$HOST_PID" ] && kill "$HOST_PID" 2>/dev/null || true
  # Wait for processes to release file locks before removing dirs.
  [ -n "$SCHEDULER_PID" ] && wait "$SCHEDULER_PID" 2>/dev/null || true
  for pid in "${INDEXER_PIDS[@]+"${INDEXER_PIDS[@]}"}"; do
    wait "$pid" 2>/dev/null || true
  done
  [ -n "$HOST_PID" ] && wait "$HOST_PID" 2>/dev/null || true
  if [ -n "$ORIG_HOST_CONFIG" ] && [ -f "$ORIG_HOST_CONFIG" ]; then
    cp "$ORIG_HOST_CONFIG" ./config/config.yaml && rm -f "$ORIG_HOST_CONFIG"
  fi
  local dir
  for dir in "${INDEXER_DIRS[@]+"${INDEXER_DIRS[@]}"}"; do
    rm -rf "$dir"
  done
  [ -n "$HOST_DIR"       ] && rm -rf "$HOST_DIR"
  [ -n "$SCHEDULER_DIR"  ] && rm -rf "$SCHEDULER_DIR"
}
trap cleanup EXIT
trap 'echo "" >&2; exit 130' INT TERM
trap 'echo "" >&2; echo "ERROR at line $LINENO" >&2' ERR

# ---------------------------------------------------------------------------
# Terminal helpers
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
  BOLD='\033[1m'; DIM='\033[2m'; GREEN='\033[0;32m'; CYAN='\033[0;36m'
  YELLOW='\033[0;33m'; RESET='\033[0m'
else
  BOLD=''; DIM=''; GREEN=''; CYAN=''; YELLOW=''; RESET=''
fi

short()  { printf '%.16s…' "$1"; }
note()   { printf "${DIM}  # %s${RESET}\n" "$1"; }
ok()     { printf "  ${GREEN}✓${RESET} %s\n" "$1"; }
hints()  {
  printf "${DIM}  try:${RESET}\n"
  for _h in "$@"; do printf "${DIM}  %s${RESET}\n" "$_h"; done
}

banner() {
  echo ""
  printf "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  printf "${BOLD}  Step %s — %s${RESET}\n" "$1" "$2"
  printf "${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${RESET}\n"
  echo ""
}

ask() {
  local prompt="$1" default="${2:-}"
  if [ -n "$default" ]; then
    printf "  ${CYAN}?${RESET}  %s ${DIM}(%s)${RESET}: " "$prompt" "$default" >&2
  else
    printf "  ${CYAN}?${RESET}  %s: " "$prompt" >&2
  fi
  local val
  read -r val || val=""
  echo "${val:-$default}"
}

# numask "prompt" min max default — validated integer input.
numask() {
  local prompt="$1" min="$2" max="$3" default="$4"
  while true; do
    printf "  ${CYAN}?${RESET}  %s ${DIM}(%d-%d, default %d)${RESET}: " "$prompt" "$min" "$max" "$default" >&2
    local val
    read -r val || val="$default"
    val="${val:-$default}"
    if [[ "$val" =~ ^[0-9]+$ ]] && [ "$val" -ge "$min" ] && [ "$val" -le "$max" ]; then
      echo "$val"; return
    fi
    printf "  ${YELLOW}!${RESET}  Enter a number between %d and %d\n" "$min" "$max" >&2
  done
}

proceed() {
  echo ""
  printf "  ${CYAN}▶${RESET}  press Enter to continue "
  read -r _ || true
}

# ---------------------------------------------------------------------------
# Binary management helpers
# ---------------------------------------------------------------------------
wait_for_http() {
  local url="$1" label="$2" log_file="${3:-}" elapsed=0
  printf "  waiting for %s" "$label"
  while ! curl -sf -H 'Accept: application/json' "$url" > /dev/null 2>&1; do
    sleep 2; elapsed=$((elapsed + 2)); printf "."
    if [ -n "$log_file" ] && [ -f "$log_file" ]; then
      if grep -q "FATAL\|level.*fatal" "$log_file" 2>/dev/null; then
        echo ""; echo "ERROR: $label crashed" >&2; tail -20 "$log_file" >&2; exit 1
      fi
    fi
    if [ "$elapsed" -ge 120 ]; then
      echo ""; echo "ERROR: $label did not start within 120s" >&2
      [ -n "$log_file" ] && [ -f "$log_file" ] && tail -20 "$log_file" >&2
      exit 1
    fi
  done
  echo " ready"
}

strip_0x() { sed 's/^0[xX]//'; }

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

# ---------------------------------------------------------------------------
# Prereq checks
# ---------------------------------------------------------------------------
for _bin in ./scheduler ./indexer ./host; do
  if [ ! -f "$_bin" ]; then
    printf "ERROR: %s not found. Build first:\n" "$_bin" >&2
    echo "  go build -o scheduler ./cmd/scheduler" >&2
    echo "  go build -o indexer   ./cmd/indexer" >&2
    echo "  go build -o host      ./cmd/host" >&2
    exit 1
  fi
done

if [ -z "${GETH_RPC_URL:-}" ] || [ -z "${GETH_WS_URL:-}" ]; then
  echo "ERROR: GETH_RPC_URL and GETH_WS_URL must be set." >&2
  exit 1
fi

if ! command -v jq > /dev/null 2>&1; then
  echo "ERROR: jq not found. Install: brew install jq (macOS) or apt install jq" >&2
  exit 1
fi

# GCP Blockchain Node Engine: embed API key in URLs if not already present.
_GETH_RPC="${GETH_RPC_URL}"
_GETH_WS="${GETH_WS_URL}"
if [ -n "${GETH_API_KEY:-}" ]; then
  [[ "$_GETH_RPC" != *"?"* ]] && _GETH_RPC="${_GETH_RPC}?key=${GETH_API_KEY}"
  [[ "$_GETH_WS"  != *"?"* ]] && _GETH_WS="${_GETH_WS}?key=${GETH_API_KEY}"
fi

# ---------------------------------------------------------------------------
# Header + initial setup
# ---------------------------------------------------------------------------
printf "\n${BOLD}${CYAN}shinzo-scheduler demo${RESET}\n"
echo ""

CHAIN=$(ask "Chain"   "${SCHEDULER_CHAIN:-ethereum}")
NETWORK=$(ask "Network" "${SCHEDULER_NETWORK:-mainnet}")
echo ""

HMAC="${SCHEDULER_HMAC_SECRET:-demo-hmac-$(date +%s)}"

# ---------------------------------------------------------------------------
# Step 1 — Start scheduler
# ---------------------------------------------------------------------------
banner 1 "Scheduler"

SCHEDULER_DIR=$(mktemp -d)
cat > "$SCHEDULER_DIR/config.yaml" <<EOF
defradb:
  url: "localhost:9191"
  keyring_secret: ""
  p2p:
    enabled: false
  store:
    path: "${SCHEDULER_DIR}/.defradb"
    block_cache_mb: 64
    memtable_mb: 64
    index_cache_mb: 32

scheduler:
  chain: ""
  network: ""
  server:
    port: 8090
    read_timeout_seconds: 30
    write_timeout_seconds: 30
  probe:
    interval_seconds: 60
    timeout_seconds: 5
    tip_lag_threshold: 10
    tip_exclusion_threshold: 50
    staleness_window_seconds: 120
    inactive_after_minutes: 10
    probe_history_limit: 200
    max_concurrent_probes: 20
    heartbeat_interval_seconds: 30
  auth:
    hmac_secret: ""
  shinzohub:
    enabled: false
    epoch_size: 0
  pricing:
    floor_tip_per_1k_blocks: 0.0
    floor_snapshot_per_range: 0.0
  diversity:
    enabled: true
    recency_window_hours: 24
  accounting:
    enabled: false
  settlement:
    enabled: false
  bootstrap:
    indexers: []

logger:
  development: false
EOF

SCHEDULER_CHAIN="$CHAIN" SCHEDULER_NETWORK="$NETWORK" \
  SCHEDULER_HMAC_SECRET="$HMAC" \
  DEFRA_KEYRING_SECRET="$KEYRING_SECRET" \
  ./scheduler --config "$SCHEDULER_DIR/config.yaml" > "$SCHEDULER_DIR/scheduler.log" 2>&1 &
SCHEDULER_PID=$!

wait_for_http "$BASE/v1/health" "scheduler" "$SCHEDULER_DIR/scheduler.log"
ok "scheduler running at $BASE  (chain: $CHAIN / $NETWORK)"
curl -sf "$BASE/v1/stats" | jq '{active_indexers, active_hosts, subscriptions}'
echo ""
hints "curl -s $BASE/v1/health | jq ." \
      "curl -s $BASE/v1/stats | jq ."

# ---------------------------------------------------------------------------
# Step 2 — Indexers
# ---------------------------------------------------------------------------
proceed
banner 2 "Indexers"

N_INDEXERS=$(numask "Number of indexers" 1 5 2)
echo ""

INDEXER_PRICES=()
for i in $(seq 1 "$N_INDEXERS"); do
  price=$(ask "Indexer #$i — price per 1k blocks" "0.50")
  INDEXER_PRICES+=("$price")
done
echo ""

# Fetch current block from Geth so indexers start at the tip, not genesis.
_block_resp=$(curl -s -w '\n%{http_code}' -X POST "$_GETH_RPC" \
  -H 'Content-Type: application/json' \
  -d '{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}')
_block_http=$(printf '%s' "$_block_resp" | tail -1)
_block_body=$(printf '%s' "$_block_resp" | sed '$d')
if [ "${_block_http:0:1}" != "2" ]; then
  printf "ERROR: Geth RPC returned HTTP %s — check GETH_RPC_URL and GETH_API_KEY\n" "$_block_http" >&2
  printf '%s\n' "$_block_body" >&2
  exit 1
fi
_block_hex=$(printf '%s' "$_block_body" | jq -r '.result // empty')
if [ -z "$_block_hex" ]; then
  printf "ERROR: unexpected Geth response (no .result): %s\n" "$_block_body" >&2
  exit 1
fi
_INDEXER_START_HEIGHT=$(printf '%d\n' "$_block_hex")

printf "  starting %d indexer(s) from block %d\n\n" "$N_INDEXERS" "$_INDEXER_START_HEIGHT"

# Start all indexers in background, then wait for all.
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  health_port=$((8091 + idx * 2))
  defra_port=$((9200 + idx))
  p2p_port=$((9172 + idx * 2))
  dir=$(mktemp -d)
  INDEXER_DIRS[$idx]="$dir"
  INDEXER_PORTS[$idx]="$health_port"

  cat > "$dir/config.yaml" <<EOF
chain:
  name: "Ethereum"
  network: "Mainnet"

defradb:
  url: "http://localhost:${defra_port}"
  keyring_secret: "${KEYRING_SECRET}-indexer-${i}"
  embedded: true
  p2p:
    enabled: true
    accept_incoming: false
    listen_addr: "/ip4/0.0.0.0/tcp/${p2p_port}"
    max_retries: 3
    retry_base_delay_ms: 1000
    reconnect_interval_ms: 30000
    enable_auto_reconnect: false
  store:
    path: "${dir}/.defra"
    block_cache_mb: 32
    memtable_mb: 16
    index_cache_mb: 16

geth:
  node_url: "${_GETH_RPC}"
  ws_url: "${_GETH_WS}"
  api_key: "${GETH_API_KEY:-}"

indexer:
  start_height: ${_INDEXER_START_HEIGHT}
  concurrent_blocks: 1
  receipt_workers: 4
  max_docs_per_txn: 100
  blocks_per_minute: 60
  health_server_port: ${health_port}
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

  ./indexer --config "$dir/config.yaml" > "$dir/indexer.log" 2>&1 &
  INDEXER_PIDS[$idx]=$!
done

# Wait for all health endpoints.
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  wait_for_http "http://localhost:${INDEXER_PORTS[$idx]}/health" "indexer-$i" "${INDEXER_DIRS[$idx]}/indexer.log"
done
echo ""

# Register each indexer with the scheduler.
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  port="${INDEXER_PORTS[$idx]}"
  dir="${INDEXER_DIRS[$idx]}"
  url="http://localhost:$port"
  price="${INDEXER_PRICES[$idx]}"

  reg=$(curl -sf -H 'Accept: application/json' "$url/registration")
  msg=$(echo "$reg" | jq -r \
    '.registration.defra_pk_registration.message //
     .registration.message //
     .message //
     empty' | strip_0x)
  defra_pk=$(echo "$reg" | jq -r '.registration.defra_pk_registration.public_key // empty' | strip_0x)
  defra_sig=$(echo "$reg" | jq -r '.registration.defra_pk_registration.signed_pk_message // empty' | strip_0x)
  p2p_addr=$(curl -sf -H 'Accept: application/json' "$url/health" \
    | jq -r '.p2p.self.addresses[0] // "/ip4/127.0.0.1/tcp/'"$((9172 + idx * 2))"'"')

  INDEXER_IDS[$idx]="$defra_pk"

  _pricing="{\"tipPer1kBlocks\": ${price}, \"snapshotPerRange\": 2.0}"
  _body=$(jq -n \
    --arg peer_id   "$defra_pk" \
    --arg defra_pk  "$defra_pk" \
    --arg msg       "$msg" \
    --arg sig       "$defra_sig" \
    --arg http_url  "$url" \
    --arg multiaddr "$p2p_addr" \
    --arg chain     "$CHAIN" \
    --arg network   "$NETWORK" \
    --arg pricing   "$_pricing" \
    '{peer_id:$peer_id,defra_pk:$defra_pk,signed_messages:{($msg):$sig},http_url:$http_url,multiaddr:$multiaddr,chain:$chain,network:$network,pricing:$pricing}')
  resp=$(post_json "$BASE/v1/indexers/register" "$_body")
  api_key=$(echo "$resp" | jq -r '.api_key')
  INDEXER_KEYS[$idx]="$api_key"

  tip=$(curl -sf -H 'Accept: application/json' "$url/health" | jq -r '.current_block // 0')
  curl -sf -X POST "$BASE/v1/indexers/$defra_pk/heartbeat" \
    -H "Authorization: Bearer $api_key" \
    -H 'Content-Type: application/json' \
    -d "{\"current_tip\": $tip, \"snapshot_ranges\": \"[]\"}" > /dev/null

  ok "indexer-$i  id=$(short "$defra_pk")  price=${price}/1k  tip=$tip"
done
echo ""
_hints=()
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  _hints+=("curl -s http://localhost:${INDEXER_PORTS[$idx]}/health | jq '{status,current_block}'")
done
hints "${_hints[@]}"

# ---------------------------------------------------------------------------
# Step 3 — Host
# ---------------------------------------------------------------------------
proceed
banner 3 "Host"

HOST_DIR=$(mktemp -d)

# Collect P2P multiaddrs from all running indexers.
_bootstrap_yaml=""
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  _addr=$(curl -sf -H 'Accept: application/json' "http://localhost:${INDEXER_PORTS[$idx]}/health" \
    | jq -r '.p2p.self.addresses[0] // empty')
  [ -n "$_addr" ] && _bootstrap_yaml="${_bootstrap_yaml}
      - \"${_addr}\""
done

cat > "$HOST_DIR/config.yaml" <<EOF
defradb:
  url: "localhost:9210"
  keyring_secret: "${KEYRING_SECRET}-host"
  p2p:
    enabled: true
    bootstrap_peers:${_bootstrap_yaml}
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
  health_server_port: 8092
  open_browser_on_start: false
  snapshot:
    enabled: false
EOF

set +e
trap '' ERR
./host --help 2>&1 | grep -q -- "--config"
_host_has_config_flag=$?
trap 'echo "" >&2; echo "ERROR at line $LINENO" >&2' ERR
set -e

if [ "$_host_has_config_flag" -eq 0 ]; then
  ./host --config "$HOST_DIR/config.yaml" > "$HOST_DIR/host.log" 2>&1 &
else
  ORIG_HOST_CONFIG="./config/host-original-$$.yaml"
  cp ./config/config.yaml "$ORIG_HOST_CONFIG"
  cp "$HOST_DIR/config.yaml" ./config/config.yaml
  ./host > "$HOST_DIR/host.log" 2>&1 &
fi
HOST_PID=$!
wait_for_http "http://localhost:8092/health" "host" "$HOST_DIR/host.log"

host_reg=$(curl -sf -H 'Accept: application/json' "http://localhost:8092/registration")
host_msg=$(echo "$host_reg" | jq -r '.registration.message // .message // empty' | strip_0x)
host_pk=$(echo "$host_reg" | jq -r '.registration.defra_pk_registration.public_key // empty' | strip_0x)
host_sig=$(echo "$host_reg" | jq -r '.registration.defra_pk_registration.signed_pk_message // empty' | strip_0x)
host_p2p=$(curl -sf -H 'Accept: application/json' "http://localhost:8092/health" \
  | jq -r '.p2p.self.addresses[0] // "/ip4/127.0.0.1/tcp/9173"')
HOST_ID="$host_pk"

_host_body=$(jq -n \
  --arg peer_id   "$HOST_ID" \
  --arg defra_pk  "$host_pk" \
  --arg msg       "$host_msg" \
  --arg sig       "$host_sig" \
  --arg http_url  "http://localhost:8092" \
  --arg multiaddr "$host_p2p" \
  --arg chain     "$CHAIN" \
  --arg network   "$NETWORK" \
  '{peer_id:$peer_id,defra_pk:$defra_pk,signed_messages:{($msg):$sig},http_url:$http_url,multiaddr:$multiaddr,chain:$chain,network:$network}')
host_resp=$(post_json "$BASE/v1/hosts/register" "$_host_body")
HOST_KEY=$(echo "$host_resp" | jq -r '.api_key')
ok "host registered  id=$(short "$HOST_ID")"
echo ""
hints "curl -s http://localhost:8092/health | jq ." \
      "curl -s $BASE/v1/stats | jq ."

# ---------------------------------------------------------------------------
# Step 4 — Discovery
# ---------------------------------------------------------------------------
proceed
banner 4 "Discovery"
DISC=$(curl -sf \
  "$BASE/v1/discover/indexers?chain=$CHAIN&network=$NETWORK&host_id=$HOST_ID" \
  -H "Authorization: Bearer $HOST_KEY")

DISC_COUNT=$(echo "$DISC" | jq 'length')
if [ "$DISC_COUNT" -eq 0 ]; then
  echo "ERROR: no indexers visible in discovery" >&2; exit 1
fi

echo ""
printf "  ${BOLD}%-4s  %-20s  %-8s  %s${RESET}\n" "#" "peer_id" "score" "price/1k"
for _idx in $(seq 0 $((DISC_COUNT - 1))); do
  _pid=$(echo "$DISC"   | jq -r ".[$_idx].peer_id")
  _score=$(echo "$DISC" | jq -r ".[$_idx].reliability_score")
  _price=$(echo "$DISC" | jq -r ".[$_idx].pricing.tipPer1kBlocks")
  printf "  %-4s  %-20s  %-8s  %s\n" "$((_idx+1))" "$(short "$_pid")" "$_score" "$_price"
done
echo ""

SELECTED_INDICES=()
if [ "$DISC_COUNT" -eq 1 ]; then
  SELECTED_INDICES=(0)
  ok "1 indexer available, auto-selected"
else
  printf "  ${CYAN}?${RESET}  Indexers to subscribe to, e.g. 1,2 ${DIM}(default 1)${RESET}: " >&2
  read -r _raw || _raw="1"
  _raw="${_raw:-1}"
  IFS=',' read -ra _parts <<< "$_raw"
  for _p in "${_parts[@]}"; do
    _p=$(echo "$_p" | tr -d ' ')
    if [[ "$_p" =~ ^[0-9]+$ ]] && [ "$_p" -ge 1 ] && [ "$_p" -le "$DISC_COUNT" ]; then
      SELECTED_INDICES+=($((_p - 1)))
    else
      printf "  ${YELLOW}!${RESET}  invalid: %s (skipped)\n" "$_p" >&2
    fi
  done
  [ ${#SELECTED_INDICES[@]} -eq 0 ] && SELECTED_INDICES=(0)
fi

SELECTED_INDEXER_IDS=()
for _si in "${SELECTED_INDICES[@]}"; do
  _sel_id=$(echo "$DISC" | jq -r ".[$_si].peer_id")
  SELECTED_INDEXER_IDS+=("$_sel_id")
  ok "selected: $(short "$_sel_id")"
done

# ---------------------------------------------------------------------------
# Step 5 — Subscribe
# ---------------------------------------------------------------------------
proceed
banner 5 "Subscribe"

for _sub_idx in "${!SELECTED_INDEXER_IDS[@]}"; do
  _cur_id="${SELECTED_INDEXER_IDS[$_sub_idx]}"
  printf "\n  ${BOLD}indexer %d/%d: $(short "$_cur_id")${RESET}\n" \
    "$((_sub_idx + 1))" "${#SELECTED_INDEXER_IDS[@]}"

  curl -sf "$BASE/v1/quotes?indexer_id=$_cur_id&type=tip&blocks=10000" \
    -H "Authorization: Bearer $HOST_KEY" \
    | jq '{indexer_id, sub_type, price_tokens, currency, valid_until}'

  _sub_resp=$(curl -sf -X POST "$BASE/v1/subscriptions" \
    -H "Authorization: Bearer $HOST_KEY" \
    -H 'Content-Type: application/json' \
    -d "{\"indexer_id\": \"$_cur_id\", \"sub_type\": \"tip\"}")
  _sub_id=$(echo "$_sub_resp" | jq -r '.subscriptionId')
  SUB_IDS+=("$_sub_id")
  echo "$_sub_resp" | jq '{subscriptionId, status, indexerId, subType}'

  EXPIRES=$(date -u -d "+90 days" +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || \
            date -u -v+90d +"%Y-%m-%dT%H:%M:%SZ")
  curl -sf -X POST "$BASE/v1/payments/verify" \
    -H "Authorization: Bearer $HOST_KEY" \
    -H 'Content-Type: application/json' \
    -d "{
      \"subscription_id\": \"$_sub_id\",
      \"payment_ref\":     \"demo-$(date +%s)-$_sub_idx\",
      \"expires_at\":      \"$EXPIRES\"
    }" | jq '{status}'

  _sub_detail=$(curl -sf "$BASE/v1/subscriptions/$_sub_id" \
    -H "Authorization: Bearer $HOST_KEY")
  echo "$_sub_detail" | jq '{
    status:            .subscription.status,
    indexer_http_url:  .indexer_http_url,
    indexer_multiaddr: .indexer_multiaddr,
    expires_at:        .subscription.expiresAt
  }'
  ok "subscribed to $(short "$_cur_id")"
done
echo ""
curl -sf "$BASE/v1/stats" | jq '{active_indexers, active_hosts, subscriptions}'

# ---------------------------------------------------------------------------
# Observe
# ---------------------------------------------------------------------------
echo ""
_hints=("tail -f $SCHEDULER_DIR/scheduler.log")
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  _hints+=("tail -f ${INDEXER_DIRS[$idx]}/indexer.log")
done
_hints+=("tail -f $HOST_DIR/host.log")
_hints+=("")
_hints+=("curl -s $BASE/v1/stats | jq .")
for i in $(seq 1 "$N_INDEXERS"); do
  idx=$((i - 1))
  _hints+=("curl -s http://localhost:${INDEXER_PORTS[$idx]}/health | jq '{status,current_block}'")
done
hints "${_hints[@]}"
echo ""

printf "  ${CYAN}▶${RESET}  press Enter to cancel and exit "
read -r _ || true

for _sub_id in "${SUB_IDS[@]}"; do
  curl -sf -X DELETE "$BASE/v1/subscriptions/$_sub_id" \
    -H "Authorization: Bearer $HOST_KEY" | jq '{status}'
done

curl -sf "$BASE/v1/stats" | jq '{active_indexers, active_hosts, subscriptions}'
echo ""
