#!/bin/sh
# Prepares the local dev environment for the VS Code F5 configuration.
#
# Dev tooling, not application code — the Go-only rule covers what ships.
#
# Two jobs, in order:
#
#   1. Refuse to continue if the port is taken. The server would otherwise
#      exit with a bare "address already in use" *after* the debugger has
#      started, and the browser opens regardless — so whatever already owns
#      the port answers instead, and the app looks broken rather than absent.
#      Failing here stops the launch and names the culprit.
#
#   2. Prepare the dev database, once. The guard is the database file, so
#      re-running never duplicates anything or fails on an existing account.
#      No working hours are created: this is a timesheet, and invented entries
#      are indistinguishable from real ones once they are in the database.

set -eu

PORT="${1:-8080}"
DB="${2:-.dev/wpcalc.db}"
# Throwaway credentials for a local, gitignored database. They are below the
# password minimum on purpose, which is why the commands below pass
# --allow-weak-password explicitly rather than the minimum being lowered.
ADMIN_NAME="admin"; ADMIN_PASS="admin"
USER_NAME="user";  USER_PASS="user"

# ---- 1. port ------------------------------------------------------------

port_in_use() {
	if command -v ss >/dev/null 2>&1; then
		ss -ltn 2>/dev/null | grep -q ":${PORT} "
	elif command -v lsof >/dev/null 2>&1; then
		lsof -iTCP:"${PORT}" -sTCP:LISTEN >/dev/null 2>&1
	else
		return 1 # cannot tell; let the server report it
	fi
}

if port_in_use; then
	echo "port ${PORT} is already in use, so wpcalc cannot start." >&2
	echo >&2

	if command -v ss >/dev/null 2>&1; then
		ss -ltnp 2>/dev/null | grep ":${PORT} " >&2 || true
	fi

	# A container published on 0.0.0.0:PORT blocks 127.0.0.1:PORT as well, and
	# the process name in ss is docker-proxy, which says nothing useful.
	if command -v docker >/dev/null 2>&1; then
		holder=$(docker ps --format '{{.Names}}\t{{.Ports}}' 2>/dev/null |
			grep ":${PORT}->" | cut -f1 || true)
		if [ -n "${holder}" ]; then
			echo >&2
			echo "  held by container: ${holder}" >&2
			echo "  free it with:      docker stop ${holder}" >&2
		fi
	fi

	echo >&2
	echo "  or change --addr in .vscode/launch.json to a free port." >&2
	exit 1
fi

# ---- 2. database --------------------------------------------------------

mkdir -p "$(dirname "${DB}")"

if [ -f "${DB}" ]; then
	echo "dev database ready: ${DB}"
	exit 0
fi

echo "preparing ${DB} …"

export CGO_ENABLED=0
export GOPRIVATE='source.simonet.internal/*'

# Placeholder employees so the grid has columns. No hours are recorded.
go run ./cmd/wpcalc sample-employees --db "${DB}" --month "$(date +%Y-%m)"

# serve refuses to start with an empty user table, so these are required, not a
# convenience. One of each role, so both permission paths can be exercised.
printf '%s\n' "${ADMIN_PASS}" |
	go run ./cmd/wpcalc user add "${ADMIN_NAME}" -role admin --allow-weak-password --db "${DB}" 2>/dev/null
printf '%s\n' "${USER_PASS}" |
	go run ./cmd/wpcalc user add "${USER_NAME}" -role user --allow-weak-password --db "${DB}" 2>/dev/null

echo "--- dev logins: ${ADMIN_NAME}/${ADMIN_PASS} (admin), ${USER_NAME}/${USER_PASS} (user) ---"
