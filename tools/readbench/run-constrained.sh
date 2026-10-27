#!/usr/bin/env bash
# Owns one disposable local PostgreSQL container; never contacts remote Docker.
set -euo pipefail
cd "$(dirname "$0")/../.."
for tool in go python3 psql pg_isready docker nginx git tar; do
  command -v "$tool" >/dev/null || { echo "Missing prerequisite: $tool" >&2; exit 1; }
done
bench_docker_host="${READBENCH_DOCKER_HOST:?Set READBENCH_DOCKER_HOST to an explicit local unix:///.../docker.sock}"
[[ "$bench_docker_host" == unix:///* ]] || { echo 'Only a local Unix Docker socket is allowed' >&2; exit 1; }
bench_root="${1:?Pass a fresh evidence directory}"
mkdir -p "$bench_root"
bench_root="$(cd "$bench_root" && pwd)"
[[ ! -e "$bench_root/baseline-source" ]] || { echo 'Use a fresh evidence directory' >&2; exit 1; }
chmod 700 "$bench_root"
if command -v caffeinate >/dev/null; then caffeinate -i -w "$$" >/dev/null 2>&1 & fi
bench_port="${READBENCH_PG_PORT:-55434}"
[[ "$bench_port" =~ ^[0-9]+$ ]] && (( bench_port >= 1024 && bench_port <= 65535 )) || { echo 'Invalid database port' >&2; exit 1; }
bench_image="${READBENCH_POSTGRES_IMAGE:-postgres@sha256:b8faeff7712663c2773a190181e1246e37e364b887611c04658c28b44e2408fe}"
bench_baseline="${READBENCH_BASELINE_REF:-d317a62a43d0e2786459cefcffd2a1ebd2369f9d}"
mkdir "$bench_root/baseline-source"
git archive "$bench_baseline" | tar -x -C "$bench_root/baseline-source"
go build -C "$bench_root/baseline-source" -o "$bench_root/baseline-app" ./tools/readbench
go build -o "$bench_root/candidate-app" ./tools/readbench
bench_container="registry-readbench-$$"
bench_volume="${bench_container}-data"
bench_docker=(docker --host "$bench_docker_host")
cleanup() {
  "${bench_docker[@]}" logs "$bench_container" > "$bench_root/postgres.log" 2>&1 || true
  "${bench_docker[@]}" rm -f "$bench_container" > "$bench_root/cleanup.log" 2>&1 || true
  "${bench_docker[@]}" volume rm "$bench_volume" >> "$bench_root/cleanup.log" 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM
"${bench_docker[@]}" run -d --name "$bench_container" --cpus=1.5 --memory=4g --memory-swap=4g --shm-size=768m \
  --mount "type=volume,source=$bench_volume,target=/var/lib/postgresql/data" \
  -p "127.0.0.1:$bench_port:5432" -e POSTGRES_USER=mcpregistry -e POSTGRES_DB=registry_bench \
  -e POSTGRES_HOST_AUTH_METHOD=trust -e 'POSTGRES_INITDB_ARGS=--locale=C --encoding=UTF8' \
  "$bench_image" -c shared_buffers=512MB -c max_connections=200 \
  -c shared_preload_libraries=pg_stat_statements -c track_io_timing=on > "$bench_root/container-id"
"${bench_docker[@]}" image inspect "$bench_image" > "$bench_root/image.json"
for _ in {1..30}; do
  if pg_isready -h 127.0.0.1 -p "$bench_port" -U mcpregistry -d registry_bench >/dev/null; then break; fi
  sleep 1
done
"${bench_docker[@]}" exec "$bench_container" cat /sys/fs/cgroup/cpu.max > "$bench_root/cpu.max"
"${bench_docker[@]}" exec "$bench_container" cat /sys/fs/cgroup/memory.max > "$bench_root/memory.max"
[[ "$(cat "$bench_root/cpu.max")" == '150000 100000' && "$(cat "$bench_root/memory.max")" == '4294967296' ]] || { echo 'Expected cgroup v2 resource limits are not active' >&2; exit 1; }
"$bench_root/candidate-app" -mode seed -namespaced -db "postgres://mcpregistry@127.0.0.1:$bench_port/registry_bench" > "$bench_root/seed.log"
cat > "$bench_root/baseline.sql" <<'SQL'
DROP INDEX IF EXISTS idx_servers_namespace_suffix;
SQL
cat > "$bench_root/candidate.sql" <<'SQL'
CREATE INDEX IF NOT EXISTS idx_servers_namespace_suffix ON servers (lower(reverse(split_part(server_name, '/', 1))) text_pattern_ops);
SQL
read -r -a bench_order <<< "${READBENCH_ORDER:-baseline candidate candidate baseline}"
python3 tools/readbench/cycles.py --baseline "$bench_root/baseline-app" --candidate "$bench_root/candidate-app" \
  --out "$bench_root/comparison" --pg-port "$bench_port" --crawlers 10 --rate 1200 \
  --search-rate "${READBENCH_SEARCH_RATE:-200}" --short-percent 0 --direct-search --diverse-keys \
  --search-scenario diverse-search --search-offering 60s --search-workers 128 \
  --pg-container "$bench_container" --docker-host "$bench_docker_host" \
  --baseline-sql "$bench_root/baseline.sql" --candidate-sql "$bench_root/candidate.sql" --order "${bench_order[@]}"
# Equivalence is outside the timed comparison and is retained independently.
psql -X -v ON_ERROR_STOP=1 -h 127.0.0.1 -p "$bench_port" -U mcpregistry -d registry_bench -f "$bench_root/candidate.sql" > "$bench_root/verification-setup.log"
python3 tools/readbench/verify-search.py --baseline "$bench_root/baseline-app" --candidate "$bench_root/candidate-app" \
  --out "$bench_root/equivalence" --pg-port "$bench_port"
echo "Evidence retained in $bench_root; owned container and volume removed on exit."
