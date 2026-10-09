#!/bin/sh
set -eu
umask 077
cd "$(dirname "$0")/.."

for command in docker ssh ssh-keygen tar; do
    command -v "$command" >/dev/null || { echo "Missing command: $command" >&2; exit 1; }
done
docker info >/dev/null
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/eva-container-check.XXXXXX")
image="eva-terminal-container-check:$(basename "$test_dir")"
container=
volume=

cleanup() {
    result=$?
    trap - EXIT HUP INT TERM
    if [ -n "$container" ]; then
        if [ "$result" -ne 0 ]; then docker logs "$container" >&2 || true; fi
        docker rm -f "$container" >/dev/null 2>&1 || true
    fi
    if [ -n "$volume" ]; then docker volume rm "$volume" >/dev/null 2>&1 || true; fi
    docker image rm "$image" >/dev/null 2>&1 || true
    rm -rf "$test_dir"
    exit "$result"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
    echo "container-check: $*" >&2
    exit 1
}

start_container() {
    container_port=$1
    container=$(docker create --stop-timeout 15 \
        --mount "type=volume,src=$volume,dst=/data" \
        --mount "type=bind,src=$test_dir/allowed.pub,dst=/etc/eva-terminal/allowlist_authorized_keys,readonly" \
        --publish "127.0.0.1::$container_port" \
        --env "SSH_ADDR=0.0.0.0:$container_port" \
        --env WOO_BASE_URL=http://127.0.0.1:1 "$image")
    docker start "$container" >/dev/null
    tries=0
    while [ "$tries" -lt 45 ]; do
        status=$(docker inspect --format '{{.State.Status}} {{.State.Health.Status}}' "$container")
        case "$status" in
            'running healthy') break ;;
            'running starting') sleep 2 ;;
            *) fail "Container failed to become healthy: $status" ;;
        esac
        tries=$((tries + 1))
    done
    [ "$status" = 'running healthy' ] || fail "Timed out waiting for SSH health"
    host_port=$(docker port "$container" "$container_port/tcp")
    host_port=${host_port##*:}
}

authenticate() {
    ssh -F /dev/null -nT -v -i "$test_dir/$1" -p "$host_port" \
        -o BatchMode=yes -o IdentitiesOnly=yes -o IdentityAgent=none \
        -o PreferredAuthentications=publickey -o ConnectTimeout=5 \
        -o StrictHostKeyChecking=accept-new \
        -o "UserKnownHostsFile=$test_dir/known_hosts" -o GlobalKnownHostsFile=/dev/null \
        terminal@127.0.0.1
}

check_auth() {
    # A non-PTY session ends immediately; the SSH log proves key authentication.
    authenticate allowed >"$test_dir/allowed.log" 2>&1 || true
    if ! grep -q 'Authenticated to .* using "publickey"' "$test_dir/allowed.log"; then
        cat "$test_dir/allowed.log" >&2
        fail "Allowlisted key did not authenticate"
    fi
    if authenticate denied >"$test_dir/denied.log" 2>&1; then
        fail "Unlisted key was accepted"
    fi
    grep -q 'Permission denied' "$test_dir/denied.log" || fail "Unlisted key did not fail authentication"
}

echo "Building the SSH runtime image..."
docker build --tag "$image" .
ssh-keygen -q -t ed25519 -N '' -f "$test_dir/allowed"
ssh-keygen -q -t ed25519 -N '' -f "$test_dir/denied"
# Public keys must be readable by the container's UID 10001.
chmod 0644 "$test_dir/allowed.pub"
volume=$(docker volume create)

echo "Checking image contents and safe defaults..."
docker run --rm --network none --entrypoint sh "$image" -ec '
    test "$(id -u):$(id -g)" = "10001:10001"
    test "$SSH_AUTH_MODE" = allowlist
    test "$CHECKOUT_ENABLED" = false
    test "$UMAMI_ENABLED" = false
    test "$SSH_ADDR" = 0.0.0.0:23234
    test -s /etc/ssl/certs/ca-certificates.crt
    test -f "$SSH_ALLOWLIST_PATH" && test ! -s "$SSH_ALLOWLIST_PATH"
    test "$(stat -c %a /data)" = 700
    test "$(stat -c %a "$STATE_DIR")" = 700
    test -w /data && test -w "$STATE_DIR"
    test -z "${EVA_BRIDGE_KEY:-}${WOO_CONSUMER_KEY:-}${WOO_CONSUMER_SECRET:-}"
    ! command -v go
'
container=$(docker create "$image")
docker export --output "$test_dir/image.tar" "$container"
tar -tf "$test_dir/image.tar" >"$test_dir/image-files"
if grep -E '(^|/)(\.env[^/]*|\.git|\.ssh_host_ed25519_key|ssh_host_ed25519_key)(/|\.|$)|\.go$|^(src|out|go|cmd|internal|wordpress|docs|testdata)/|^data/state/.+' "$test_dir/image-files"; then
    fail "Source, secrets or private state found in the runtime image"
fi
docker rm "$container" >/dev/null
container=

echo "Checking SSH access, health and private storage..."
start_container 23234
check_auth
health_command=$(docker inspect --format '{{index .Config.Healthcheck.Test 1}}' "$container")
docker exec "$container" sh -ec "$health_command"
if docker exec --env SSH_ADDR=0.0.0.0:1 "$container" sh -ec "$health_command"; then
    fail "Health check accepted an unused port"
fi
host_key=$(ssh-keygen -F "[127.0.0.1]:$host_port" -f "$test_dir/known_hosts" | awk '!/^#/ {print $2, $3}')
[ -n "$host_key" ] || fail "SSH host identity was not recorded"
docker exec "$container" sh -ec '
    test "$(id -u):$(id -g)" = "10001:10001"
    test "$(cat /proc/1/comm)" = woossh
    test "$(stat -c %a:%u:%g "$SSH_HOSTKEY_PATH")" = 600:10001:10001
    test "$(stat -c %a:%u:%g /data)" = 700:10001:10001
    test "$(stat -c %a:%u:%g "$STATE_DIR")" = 700:10001:10001
    umask 077
    printf "persisted sample state\n" > "$STATE_DIR/container-check.txt"
    test "$(stat -c %a "$STATE_DIR/container-check.txt")" = 600
'

echo "Replacing the container with the same volume and a different SSH port..."
docker stop --time 15 "$container" >/dev/null
[ "$(docker inspect --format '{{.State.ExitCode}}' "$container")" = 0 ] || fail "SIGTERM did not stop the service cleanly"
docker rm "$container" >/dev/null
container=
start_container 23235
check_auth
docker exec "$container" sh -ec "$health_command"
restored_host_key=$(ssh-keygen -F "[127.0.0.1]:$host_port" -f "$test_dir/known_hosts" | awk '!/^#/ {print $2, $3}')
[ "$restored_host_key" = "$host_key" ] || fail "SSH host identity changed"
docker exec "$container" sh -ec '
    test "$(cat "$STATE_DIR/container-check.txt")" = "persisted sample state"
    test "$(stat -c %a:%u:%g /data)" = 700:10001:10001
    test "$(stat -c %a:%u:%g "$STATE_DIR")" = 700:10001:10001
    test "$(stat -c %a:%u:%g "$SSH_HOSTKEY_PATH")" = 600:10001:10001
    test "$(stat -c %a:%u:%g "$STATE_DIR/container-check.txt")" = 600:10001:10001
    umask 077
    printf "writable after replacement\n" >> "$STATE_DIR/container-check.txt"
'
echo "container-check: passed"
