#!/usr/bin/env bash
# Starts only disposable local processes. Never uses kubectl, cloud credentials, or staging.
set -euo pipefail
cd "$(dirname "$0")/../.."
for tool in go python3 initdb pg_ctl psql createdb nginx; do
  command -v "$tool" >/dev/null || { echo "Missing local prerequisite: $tool" >&2; exit 1; }
done
run_root="${1:-$(mktemp -d "${TMPDIR:-/tmp}/registry-readbench.XXXXXX")}"
mkdir -p "$run_root"
run_root="$(cd "$run_root" && pwd)"
chmod 700 "$run_root"
if [[ -e "$run_root/pgdata" ]]; then
  echo "Use a fresh result directory; refusing to reuse $run_root/pgdata" >&2
  exit 1
fi
pg_port="${READBENCH_PG_PORT:-55432}"
if [[ ! "$pg_port" =~ ^[0-9]+$ ]] || (( pg_port < 1024 || pg_port > 65535 )); then
  echo "READBENCH_PG_PORT must be an unprivileged TCP port" >&2
  exit 1
fi
cleanup() {
  pg_ctl -D "$run_root/pgdata" -m fast stop > "$run_root/stop.log" 2>&1 || true
}
initdb -D "$run_root/pgdata" -U mcpregistry -A trust --no-locale > "$run_root/init.log"
trap cleanup EXIT INT TERM
pg_ctl -D "$run_root/pgdata" -l "$run_root/postgres.log" \
  -o "-p $pg_port -h 127.0.0.1 -k /tmp -c shared_buffers=512MB -c max_connections=200 -c shared_preload_libraries=pg_stat_statements -c track_io_timing=on" start
createdb -h 127.0.0.1 -p "$pg_port" -U mcpregistry registry_bench
go build -o "$run_root/readbench" ./tools/readbench
"$run_root/readbench" -mode seed -db "postgres://mcpregistry@127.0.0.1:$pg_port/registry_bench" | tee "$run_root/seed.log"
python3 tools/readbench/compare.py --binary "$run_root/readbench" --out "$run_root/statistics" --pg-port "$pg_port" --duration "${READBENCH_DURATION:-20s}"
python3 tools/readbench/cache.py --binary "$run_root/readbench" --out "$run_root/cache" --pg-port "$pg_port"
python3 tools/readbench/verify.py --binary "$run_root/readbench" --out "$run_root/verification" --pg-port "$pg_port"
echo "Results: $run_root (database stopped; files retained for inspection)"
