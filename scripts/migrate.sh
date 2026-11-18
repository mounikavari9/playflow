#!/bin/bash
# Runs Goose migrations against the local PlayFlow Compose database only.
#
# Usage: bash scripts/migrate.sh validate|status|up|down
# Normally run through the Makefile: make migrate-status, migrate-up, migrate-down.
#
# The connection details come from the running PostgreSQL container, never from
# DATABASE_URL, so a URL exported in your shell cannot be migrated by accident.
# Every check fails closed: if the target cannot be verified, nothing runs.
# Passwords and connection strings are never printed.
#
# Written for Bash 3.2, the version that ships with macOS.

# Restart in a clean shell before touching any credential. This drops inherited
# tracing (bash -x, SHELLOPTS, BASH_ENV), exported shell functions, CDPATH and
# every GOOSE_*, PG* and *_URL setting. exec keeps the process ID, so the marker
# below only matches the restarted shell.
set +xv
if [ "${PLAYFLOW_MIGRATE_SHELL:-}" != "$$" ]; then
	clean_env=("PLAYFLOW_MIGRATE_SHELL=$$" "PATH=$PATH")
	for name in HOME USER LOGNAME TMPDIR TERM LANG LC_ALL \
		COMPOSE_PROJECT_NAME DOCKER_HOST DOCKER_CONTEXT DOCKER_CONFIG \
		GOPATH GOMODCACHE GOCACHE GOPROXY GOSUMDB GOPRIVATE GONOPROXY GONOSUMDB \
		HTTP_PROXY HTTPS_PROXY NO_PROXY SSL_CERT_FILE SSL_CERT_DIR; do
		if [ -n "${!name+set}" ]; then
			clean_env+=("$name=${!name}")
		fi
	done
	exec /usr/bin/env -i "${clean_env[@]}" /bin/bash --noprofile --norc "$0" "$@"
fi

set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)
readonly ROOT
readonly COMPOSE_FILE_PATH="$ROOT/docker-compose.yml"
readonly GOOSE_PACKAGE="github.com/pressly/goose/v3/cmd/goose@v3.27.0"
readonly DATA_DIR="/var/lib/postgresql/data"
readonly URL_SAFE='^[A-Za-z0-9._~-]+$'
readonly USAGE="Usage: bash scripts/migrate.sh validate|status|up|down"

PROJECT=${COMPOSE_PROJECT_NAME:-playflow}
container=""
db_user=""
db_password=""
db_name=""
db_port=""
lock_path=""

die() {
	printf 'Error: %s\n' "$*" >&2
	exit 1
}

release_lock() {
	if [ -n "$lock_path" ]; then
		rm -f "$lock_path"
		lock_path=""
	fi
}

# Allows one changing migration command per Compose project on this machine. The
# lock is a symlink naming the owner's process ID, which is created atomically.
acquire_lock() {
	local path owner attempt
	path=${TMPDIR:-/tmp}
	path="${path%/}/playflow-migrate-$PROJECT.lock"
	for attempt in 1 2; do
		if ln -s "$$" "$path" 2>/dev/null; then
			lock_path=$path
			trap release_lock EXIT
			trap 'exit 130' INT
			trap 'exit 143' TERM
			return 0
		fi
		owner=$(readlink "$path" 2>/dev/null || true)
		if [[ "$owner" =~ ^[0-9]+$ ]] && kill -0 "$owner" 2>/dev/null; then
			die "Another migration command is running (process $owner). Wait for it to finish."
		fi
		rm -f "$path"
	done
	die "Could not lock migrations for project $PROJECT."
}

# Hides the password and any connection string in Goose output.
redact() {
	local line
	while IFS= read -r line || [ -n "$line" ]; do
		if [ -n "$db_password" ]; then
			line=${line//"$db_password"/[REDACTED]}
		fi
		printf '%s\n' "$line"
	done | sed -E \
		-e 's#postgres(ql)?://[^[:space:]]*#[REDACTED]#g' \
		-e 's#([Pp][Aa][Ss][Ss][Ww][Oo][Rr][Dd])=[^[:space:]]*#\1=[REDACTED]#g'
}

# Runs Goose. The connection string travels in the environment of this one
# process, so it never appears in command arguments.
run_goose() {
	(
		cd "$ROOT/backend"
		export GOTOOLCHAIN=local GOFLAGS=
		if [ -n "$db_port" ]; then
			export GOOSE_DRIVER=postgres
			export GOOSE_DBSTRING="postgres://$db_user:$db_password@127.0.0.1:$db_port/$db_name?sslmode=disable"
		fi
		exec go run "$GOOSE_PACKAGE" -env=none -dir migrations "$@"
	) 2>&1 | redact
}

verify_project() {
	case "$PROJECT" in
	playflow | playflow-test) ;;
	*) die "COMPOSE_PROJECT_NAME must be playflow or playflow-test." ;;
	esac
}

verify_local_docker() {
	local endpoint
	case "${DOCKER_HOST:-}" in
	"" | unix://*) ;;
	*) die "DOCKER_HOST points to a non-local Docker engine. Migrations only run locally." ;;
	esac
	endpoint=$(docker context inspect --format '{{.Endpoints.docker.Host}}' 2>/dev/null) ||
		die "Could not verify the Docker context."
	case "$endpoint" in
	unix://*) ;;
	*) die "The current Docker context is not local. Migrations only run locally." ;;
	esac
}

# Confirms that the container belongs to this repository's Compose project.
verify_container() {
	local ids info label_project label_service label_dir image images status health mount real_dir
	ids=$(docker compose -p "$PROJECT" -f "$COMPOSE_FILE_PATH" ps -q postgres 2>/dev/null) ||
		die "Could not read the Compose project. Is Docker running?"
	[ -n "$ids" ] || die "PostgreSQL is not running. Start it with: make up"
	[ "$(printf '%s\n' "$ids" | wc -l | tr -d ' ')" = "1" ] ||
		die "Expected exactly one PostgreSQL container in project $PROJECT."
	container=$ids

	info=$(docker inspect -f '{{index .Config.Labels "com.docker.compose.project"}}|{{index .Config.Labels "com.docker.compose.service"}}|{{index .Config.Labels "com.docker.compose.project.working_dir"}}|{{.Config.Image}}|{{.State.Status}}|{{.State.Health.Status}}' "$container" 2>/dev/null) ||
		die "Could not verify the PostgreSQL container."
	IFS='|' read -r label_project label_service label_dir image status health <<<"$info"

	[ "$label_project" = "$PROJECT" ] || die "The PostgreSQL container belongs to a different Compose project."
	[ "$label_service" = "postgres" ] || die "The container is not the postgres service."
	real_dir=$(cd "$label_dir" 2>/dev/null && pwd -P) ||
		die "PostgreSQL was started from a folder that no longer exists. Run make down, then make up."
	[ "$real_dir" = "$ROOT" ] ||
		die "PostgreSQL was started from a different folder. Run make down, then make up from this folder."
	images=$(docker compose -p "$PROJECT" -f "$COMPOSE_FILE_PATH" config --images 2>/dev/null) ||
		die "Could not read the expected PostgreSQL image."
	grep -Fxq "$image" <<<"$images" || die "The PostgreSQL container uses an unexpected image."
	[ "$status" = "running" ] || die "PostgreSQL is not running. Start it with: make up"
	[ "$health" = "healthy" ] || die "PostgreSQL is not healthy yet. Check it with: make ps"

	mount=$(docker inspect -f "{{range .Mounts}}{{if eq .Destination \"$DATA_DIR\"}}{{.Type}}:{{.Name}}{{end}}{{end}}" "$container" 2>/dev/null) ||
		die "Could not verify the PostgreSQL data volume."
	[ "$mount" = "volume:${PROJECT}_postgres-data" ] || die "PostgreSQL is not using the expected data volume."
}

# Proves that the published port reaches this exact container. It opens a
# connection, sends a PostgreSQL startup message with a random user name and no
# credentials, and looks for that name in the container's process list. A
# different program or database on the port can never make the name appear there.
prove_port_reaches_container() {
	local marker length attempt processes found=no
	marker="playflow_verify_${RANDOM}${RANDOM}${RANDOM}"
	length=$((4 + 4 + 5 + ${#marker} + 1 + 9 + ${#marker} + 1 + 1))

	(exec 3<>"/dev/tcp/127.0.0.1/$db_port") 2>/dev/null ||
		die "Nothing is accepting connections on port $db_port."
	exec 3<>"/dev/tcp/127.0.0.1/$db_port"
	printf "\\000\\000\\000\\$(printf '%03o' "$length")\\000\\003\\000\\000user\\000%s\\000database\\000%s\\000\\000" "$marker" "$marker" >&3 ||
		die "Could not verify port $db_port."

	for attempt in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
		processes=$(docker exec "$container" sh -c 'cat /proc/[0-9]*/cmdline 2>/dev/null' 2>/dev/null | tr '\000' ' ' || true)
		case "$processes" in
		*"$marker"*)
			found=yes
			break
			;;
		esac
		sleep 0.1
	done
	exec 3>&-

	[ "$found" = "yes" ] ||
		die "Port $db_port does not reach the PlayFlow PostgreSQL container. Stop the other program or set POSTGRES_PORT."
}

# Confirms a single loopback port, then proves where it leads.
verify_port() {
	local bindings
	bindings=$(docker inspect -f '{{range $b := index .NetworkSettings.Ports "5432/tcp"}}{{$b.HostIp}}:{{$b.HostPort}}{{"\n"}}{{end}}' "$container" 2>/dev/null) ||
		die "Could not verify the published PostgreSQL port."
	bindings=$(printf '%s\n' "$bindings" | sed '/^$/d')
	[ "$(printf '%s\n' "$bindings" | wc -l | tr -d ' ')" = "1" ] ||
		die "PostgreSQL must publish exactly one port."
	[[ "$bindings" =~ ^127\.0\.0\.1:[0-9]+$ ]] || die "PostgreSQL must be published on 127.0.0.1 only."
	db_port=${bindings##*:}
	prove_port_reaches_container
}

# Reads one value from the verified container. The value is never printed.
read_container_env() {
	local value
	value=$(docker exec "$container" printenv "$1" 2>/dev/null) || die "Could not read $1 from the PostgreSQL container."
	[[ "$value" =~ $URL_SAFE ]] || die "$1 must contain only letters, digits, dot, underscore, tilde or hyphen."
	printf '%s' "$value"
}

# Credentials are read only after the port has been proven.
verify_target() {
	verify_project
	verify_local_docker
	verify_container
	verify_port
	db_user=$(read_container_env POSTGRES_USER)
	db_password=$(read_container_env POSTGRES_PASSWORD)
	db_name=$(read_container_env POSTGRES_DB)
}

print_target() {
	printf 'Target: project %s, database %s on 127.0.0.1:%s\n' "$PROJECT" "$db_name" "$db_port"
}

current_version() {
	local output pattern='goose: version ([0-9]+)'
	output=$(run_goose version) || die "Could not read the current migration version."
	[[ "$output" =~ $pattern ]] || die "Could not read the current migration version."
	printf '%s' "${BASH_REMATCH[1]}"
}

rollback() {
	local version padded matches answer verified_container
	[ -t 0 ] || die "make migrate-down needs an interactive terminal. No changes were made."

	version=$(current_version)
	if [ "$version" = "0" ]; then
		echo "No migrations are applied, so there is nothing to roll back."
		return 0
	fi
	padded=$(printf '%05d' "$((10#$version))")
	matches=$(cd "$ROOT/backend/migrations" && ls -1 "${padded}"_*.sql 2>/dev/null || true)
	[ -n "$matches" ] && [ "$(printf '%s\n' "$matches" | wc -l | tr -d ' ')" = "1" ] ||
		die "Could not find exactly one file for migration $version. No changes were made."

	echo
	echo "This rolls back one migration: $matches"
	echo "Its Down section runs now and can permanently delete data."
	# An empty IFS keeps spaces, so the answer must match exactly.
	IFS= read -r -p "Type the version number $version to continue: " answer || answer=""
	if [ "$answer" != "$version" ]; then
		echo "Rollback cancelled. No changes were made."
		return 1
	fi

	# The prompt can stay open for a while, so verify everything again.
	verified_container=$container
	verify_target
	[ "$container" = "$verified_container" ] || die "The PostgreSQL container changed. No changes were made."
	[ "$(current_version)" = "$version" ] || die "The migration version changed. No changes were made."
	run_goose down
}

main() {
	[ "$#" -eq 1 ] || die "$USAGE"
	command -v go >/dev/null 2>&1 || die "Go is not installed."

	case "$1" in
	validate)
		run_goose validate
		;;
	status)
		verify_target
		print_target
		run_goose status
		;;
	up)
		verify_project
		acquire_lock
		verify_target
		print_target
		run_goose up
		;;
	down)
		verify_project
		acquire_lock
		verify_target
		print_target
		rollback
		;;
	*)
		die "$USAGE"
		;;
	esac
}

main "$@"
