#!/usr/bin/env bash
set -euo pipefail

SOCKET="${NEXUSFLOW_PLASMA_SOCK:-/run/plasma.sock}"
rm -f "${SOCKET}"

echo "starting Plasma coordinator on unix:${SOCKET}"
/usr/local/bin/nexusflow plasma run \
  --listen "${SOCKET}" \
  --file /demo/plasma.yaml \
  --shm-name dockdemo \
  --shm-size 65536 \
  --idle-exit 5m &
COORD_PID=$!

for _ in $(seq 1 200); do
  if [[ -S "${SOCKET}" ]]; then
    break
  fi
  sleep 0.05
done

if [[ ! -S "${SOCKET}" ]]; then
  echo "coordinator socket missing" >&2
  kill "${COORD_PID}" 2>/dev/null || true
  exit 1
fi

echo "socket ready — bootstrap → branch(leaf_dynamic) runs automatically via YAML."

/usr/local/bin/nexusflow dashboard --listen 0.0.0.0:9842 &
DASH_PID=$!

cleanup() {
  kill "${COORD_PID:-}" 2>/dev/null || true
  kill "${DASH_PID:-}" 2>/dev/null || true
}
trap cleanup SIGTERM SIGINT EXIT

echo "dashboard: http://127.0.0.1:9842/ (container stays up until docker compose stop)"
echo "attach shell: docker compose exec -it plasma-demo bash"

# Wait on dashboard only: Plasma may exit on idle-exit while UI stays up.
wait "${DASH_PID}"
