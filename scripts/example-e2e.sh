#!/usr/bin/env bash
#
# Runs both examples against a real Nacos:
#
#   * example/simple reads three dataIds (one of them from another group and
#     another one from another namespace) and prints the merged config together
#     with the description of every key it read.
#   * example/watch keeps listening and has to report a live change.
#
# The Nacos instance is the one from docker-compose.yml, normally started with
# `make nacos-up && make nacos-wait`. The script is invoked by `make example-e2e`
# and cleans up the processes it started, not the Nacos container.
set -euo pipefail

NACOS_ADDR="${NACOS_ADDR:-127.0.0.1:8848}"
BASE_URL="http://${NACOS_ADDR}/nacos"

GROUP="DEFAULT_GROUP"
DB_GROUP="DATABASE"
REDIS_NAMESPACE="middleware"

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK_DIR="$(mktemp -d)"
WATCH_PID=""

cleanup() {
	if [[ -n "${WATCH_PID}" ]]; then
		kill -TERM "${WATCH_PID}" 2>/dev/null || true
		wait "${WATCH_PID}" 2>/dev/null || true
	fi
	rm -rf "${WORK_DIR}"
}
trap cleanup EXIT

log() { echo "==> $*"; }

fail() {
	echo "example-e2e: $*" >&2
	exit 1
}

# dsn_for <dir> builds a DSN with private SDK log and cache directories.
dsn_for() {
	echo "nacos://${NACOS_ADDR}?group=${GROUP}&logDir=$1/log&cacheDir=$1/cache&logLevel=error&notLoadCacheAtStart=true"
}

require_ready() {
	curl -fsS "${BASE_URL}/v1/console/health/readiness" >/dev/null 2>&1 ||
		fail "Nacos at ${NACOS_ADDR} is not ready, run 'make nacos-up && make nacos-wait' first"
}

# publish <dataId> <group> <content> [namespace]
publish() {
	local data_id="$1" group="$2" content="$3" namespace="${4:-}"
	local args=(-sS -o /dev/null -w '%{http_code}' -X POST "${BASE_URL}/v1/cs/configs"
		--data-urlencode "dataId=${data_id}"
		--data-urlencode "group=${group}"
		--data-urlencode "content=${content}")
	if [[ -n "${namespace}" ]]; then
		args+=(--data-urlencode "tenant=${namespace}")
	fi

	local code
	code="$(curl "${args[@]}")"
	[[ "${code}" == "200" ]] ||
		fail "publishing ${data_id} (group ${group}, namespace ${namespace:-public}) returned HTTP ${code}"

	echo "    published ${data_id} (group ${group}, namespace ${namespace:-public})"
}

# ensure_namespace <id>
ensure_namespace() {
	local id="$1" code
	code="$(curl -sS -o /dev/null -w '%{http_code}' -X POST "${BASE_URL}/v1/console/namespaces" \
		--data-urlencode "customNamespaceId=${id}" \
		--data-urlencode "namespaceName=${id}" \
		--data-urlencode "namespaceDesc=cleannacos example")"

	# A fresh container answers 200; an existing namespace is reported as an
	# error, which is fine because the publishes below verify it is usable.
	echo "    namespace ${id} reported HTTP ${code}"
}

# wait_for_output <file> <needle> <attempts> <what>
wait_for_output() {
	local file="$1" needle="$2" attempts="$3" what="$4"

	for _ in $(seq 1 "${attempts}"); do
		if grep -qF "${needle}" "${file}"; then
			return 0
		fi
		if [[ -n "${WATCH_PID}" ]] && ! kill -0 "${WATCH_PID}" 2>/dev/null; then
			break
		fi
		sleep 0.5
	done

	echo "--- captured output ---" >&2
	cat "${file}" >&2
	echo "-----------------------" >&2
	fail "${what}"
}

require_ready

log "preparing configs in Nacos"
ensure_namespace "${REDIS_NAMESPACE}"
publish "server.yaml" "${GROUP}" "addr: 0.0.0.0:8080
debug: true"
publish "db.yaml" "${DB_GROUP}" "host: db.internal
password: secret"
publish "redis.yaml" "${GROUP}" "addr: redis.internal:6379"
publish "redis.yaml" "${GROUP}" "addr: redis.internal:6379" "${REDIS_NAMESPACE}"

log "building the examples"
(cd "${REPO_ROOT}" && go build -o "${WORK_DIR}/example-simple" ./example/simple)
(cd "${REPO_ROOT}" && go build -o "${WORK_DIR}/example-watch" ./example/watch)

log "running example/simple"
SIMPLE_OUT="${WORK_DIR}/simple.out"
if ! CLEANNACOS_DSN="$(dsn_for "${WORK_DIR}/simple")" "${WORK_DIR}/example-simple" >"${SIMPLE_OUT}" 2>&1; then
	cat "${SIMPLE_OUT}" >&2
	fail "example/simple exited with an error"
fi
cat "${SIMPLE_OUT}"

# Values from Nacos, values from nacos-default, and the description output.
for want in \
	"Addr:0.0.0.0:8080" \
	"Timeout:5s" \
	"VirtualHosts:[api.local ops.local]" \
	"2026-09-17" \
	"Host:db.internal" \
	"Password:secret" \
	"Port:5432" \
	"Tags:map" \
	"Addr:redis.internal:6379" \
	"server.yaml:addr" \
	"db.yaml:host" \
	"redis.yaml:addr" \
	"listen address" \
	"accepted host names"; do
	if ! grep -qF "${want}" "${SIMPLE_OUT}"; then
		cat "${SIMPLE_OUT}" >&2
		fail "example/simple output does not contain ${want}"
	fi
done
log "example/simple printed the merged config and its description"

log "running example/watch"
WATCH_OUT="${WORK_DIR}/watch.out"
CLEANNACOS_DSN="$(dsn_for "${WORK_DIR}/watch")" "${WORK_DIR}/example-watch" >"${WATCH_OUT}" 2>&1 &
WATCH_PID=$!

wait_for_output "${WATCH_OUT}" "baseline:" 120 "example/watch did not deliver a baseline snapshot"
grep -F "baseline:" "${WATCH_OUT}" | head -1

log "changing server.yaml in Nacos"
publish "server.yaml" "${GROUP}" "addr: 0.0.0.0:9090
debug: true"

wait_for_output "${WATCH_OUT}" "0.0.0.0:9090" 120 "example/watch did not report the changed address"
grep -F "0.0.0.0:9090" "${WATCH_OUT}" | head -1
log "example/watch reported the change"

log "stopping example/watch"
kill -TERM "${WATCH_PID}"
# Never block forever on a stuck process.
(sleep 20 && kill -KILL "${WATCH_PID}" 2>/dev/null) &
WATCHDOG_PID=$!
wait "${WATCH_PID}" || true
kill "${WATCHDOG_PID}" 2>/dev/null || true
wait "${WATCHDOG_PID}" 2>/dev/null || true
WATCH_PID=""

grep -qF "stopping" "${WATCH_OUT}" || fail "example/watch did not shut down gracefully on SIGTERM"
log "example/watch shut down gracefully"

log "example end-to-end verification passed"
