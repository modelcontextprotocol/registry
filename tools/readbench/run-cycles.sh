#!/usr/bin/env bash
# Owns disposable local processes only; never contacts staging/production.
set -euo pipefail
cd "$(dirname "$0")/../.."
for tool in go python3 initdb pg_ctl psql createdb nginx git tar; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
# macOS stops its monotonic clock during sleep; keep this benchmark awake only
# for this script's lifetime. Timing guards still reject other clock changes.
if command -v caffeinate >/dev/null; then
  caffeinate -i -w "$$" >/dev/null 2>&1 &
fi
run_root="${1:-$(mktemp -d /tmp/registry-cycles.XXXXXX)}"
mkdir -p "$run_root"
run_root="$(cd "$run_root" && pwd)"
chmod 700 "$run_root"
[[ ! -e "$run_root/pgdata" ]] || { echo 'Use a fresh output directory' >&2; exit 1; }
pg_port="${READBENCH_PG_PORT:-55433}"
if [[ ! "$pg_port" =~ ^[0-9]+$ ]] || (( pg_port < 1024 || pg_port > 65535 )); then
  echo 'Invalid READBENCH_PG_PORT' >&2; exit 1
fi
# Initial harness commit: same application behavior as the deployed baseline,
# with the local-only serve wrapper. Override explicitly for future comparisons.
baseline_ref="${READBENCH_BASELINE_REF:-e19fda006538296bbdb63be1fb22195d66813928}"
mkdir "$run_root/baseline-source"
git archive "$baseline_ref" | tar -x -C "$run_root/baseline-source"
go build -C "$run_root/baseline-source" -o "$run_root/baseline-app" ./tools/readbench
go build -o "$run_root/candidate-app" ./tools/readbench
initdb -D "$run_root/pgdata" -U mcpregistry -A trust --no-locale > "$run_root/init.log"
cleanup() { pg_ctl -D "$run_root/pgdata" -m fast stop > "$run_root/stop.log" 2>&1 || true; }
trap cleanup EXIT INT TERM
pg_ctl -D "$run_root/pgdata" -l "$run_root/postgres.log" -o "-p $pg_port -h 127.0.0.1 -k /tmp -c shared_buffers=512MB -c max_connections=200 -c shared_preload_libraries=pg_stat_statements -c track_io_timing=on" start
createdb -h 127.0.0.1 -p "$pg_port" -U mcpregistry registry_bench
"$run_root/candidate-app" -mode seed -namespaced -db "postgres://mcpregistry@127.0.0.1:$pg_port/registry_bench" | tee "$run_root/seed.log"
read -r -a run_order <<< "${READBENCH_ORDER:-baseline candidate candidate baseline}"
python3 tools/readbench/cycles.py --baseline "$run_root/baseline-app" --candidate "$run_root/candidate-app" --out "$run_root/comparison" --pg-port "$pg_port" --crawlers "${READBENCH_CRAWLERS:-20}" --rate "${READBENCH_CRAWL_RPS:-1200}" --short-percent "${READBENCH_SHORT_PERCENT:-1}" --search-rate "${READBENCH_SEARCH_RATE:-10}" --publish-rate "${READBENCH_PUBLISH_RATE:-1}" --order "${run_order[@]}"
echo "Results retained in $run_root; local database stopped on exit."
