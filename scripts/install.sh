#!/bin/sh
# Idempotent Linux/systemd installer for Atalaya.
# Production defaults are fixed; ATALAYA_ROOT and the other ATALAYA_* path
# overrides are intended for staged/offline tests.
set -eu

ROLE=
VERSION=
ARTIFACT_DIR=
BASE_URL="https://github.com/mkrebs03/atalaya/releases"
NO_START=0
SERVER_URL="${ATALAYA_SERVER_URL:-}"
AGENT_URL="${ATALAYA_AGENT_URL:-}"
DISPLAY_NAME="${ATALAYA_DISPLAY_NAME:-}"

usage() {
  cat <<'EOF'
Usage: install.sh <server|agent|upgrade> [options]

Options:
  --version <version>       Release version, for example v0.1.0
  --artifact-dir <path>     Offline directory containing binary and SHA256SUMS
  --base-url <url>          Release base URL
  --server-url <url>        Server origin for a new agent.yaml
  --agent-url <url>         Agent advertised URL for a new agent.yaml
  --display-name <name>     Local agent display name
  --no-start                Install files and units without migrate/start/restart
EOF
}

die() {
  printf '%s\n' "atalaya install: $*" >&2
  exit 1
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    server|agent|upgrade)
      [ -z "$ROLE" ] || die "only one role may be selected"
      ROLE=$1
      shift
      ;;
    --version)
      [ "$#" -ge 2 ] || die "--version requires a value"
      VERSION=$2
      shift 2
      ;;
    --version=*)
      VERSION=${1#--version=}
      shift
      ;;
    --artifact-dir)
      [ "$#" -ge 2 ] || die "--artifact-dir requires a value"
      ARTIFACT_DIR=$2
      shift 2
      ;;
    --artifact-dir=*)
      ARTIFACT_DIR=${1#--artifact-dir=}
      shift
      ;;
    --base-url)
      [ "$#" -ge 2 ] || die "--base-url requires a value"
      BASE_URL=$2
      shift 2
      ;;
    --base-url=*)
      BASE_URL=${1#--base-url=}
      shift
      ;;
    --server-url)
      [ "$#" -ge 2 ] || die "--server-url requires a value"
      SERVER_URL=$2
      shift 2
      ;;
    --agent-url)
      [ "$#" -ge 2 ] || die "--agent-url requires a value"
      AGENT_URL=$2
      shift 2
      ;;
    --display-name)
      [ "$#" -ge 2 ] || die "--display-name requires a value"
      DISPLAY_NAME=$2
      shift 2
      ;;
    --no-start)
      NO_START=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1"
      ;;
  esac
done

[ -n "$ROLE" ] || { usage >&2; exit 2; }
[ "$(uname -s)" = Linux ] || die "Linux with systemd is required"
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture $(uname -m)" ;;
esac

ROOT_PREFIX=${ATALAYA_ROOT:-}
if [ -n "$ROOT_PREFIX" ]; then
  case "$ROOT_PREFIX" in
    /*) ;;
    *) die "ATALAYA_ROOT must be absolute" ;;
  esac
fi
prefix_path() {
  if [ -n "$ROOT_PREFIX" ]; then
    printf '%s%s\n' "$ROOT_PREFIX" "$1"
  else
    printf '%s\n' "$1"
  fi
}
INSTALL_BIN=${ATALAYA_BIN:-$(prefix_path /usr/local/bin/atalaya)}
ETC_DIR=${ATALAYA_ETC_DIR:-$(prefix_path /etc/atalaya)}
STATE_DIR=${ATALAYA_STATE_DIR:-$(prefix_path /var/lib/atalaya)}
SYSTEMD_DIR=${ATALAYA_SYSTEMD_DIR:-$(prefix_path /etc/systemd/system)}
SYSTEMCTL=${ATALAYA_SYSTEMCTL:-systemctl}
TAILSCALE=${ATALAYA_TAILSCALE:-tailscale}
SKIP_USER=${ATALAYA_SKIP_SYSTEM_USER:-0}

[ "$(id -u)" -eq 0 ] || [ -n "$ROOT_PREFIX" ] || [ "${ATALAYA_INSTALL_ALLOW_NONROOT:-0}" = 1 ] ||
  die "run as root (use ATALAYA_ROOT only for a staged test)"

ARTIFACT_NAME="atalaya-linux-$ARCH"
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/atalaya-install.XXXXXX")
cleanup() { rm -rf "$TMP_DIR"; }
trap cleanup EXIT HUP INT TERM

download() {
  destination=$1
  source=$2
  case "$source" in
    file://*) cp "${source#file://}" "$destination" ;;
    *)
      case "$source" in
        https://*) ;;
        *) die "release downloads require HTTPS" ;;
      esac
      if command -v curl >/dev/null 2>&1; then
        curl --fail --silent --show-error --location --retry 3 --proto '=https' --proto-redir '=https' --output "$destination" "$source"
      elif command -v wget >/dev/null 2>&1; then
        wget --quiet --https-only --output-document="$destination" "$source"
      else
        die "curl or wget is required"
      fi
      ;;
  esac
}

if [ -n "$ARTIFACT_DIR" ]; then
  [ -f "$ARTIFACT_DIR/$ARTIFACT_NAME" ] || die "offline artifact is missing $ARTIFACT_NAME"
  [ -f "$ARTIFACT_DIR/SHA256SUMS" ] || die "offline artifact is missing SHA256SUMS"
  cp "$ARTIFACT_DIR/$ARTIFACT_NAME" "$TMP_DIR/$ARTIFACT_NAME"
  cp "$ARTIFACT_DIR/SHA256SUMS" "$TMP_DIR/SHA256SUMS"
else
  [ -n "$VERSION" ] || VERSION=latest
  RELEASE_ROOT=$(printf '%s\n' "$BASE_URL" | sed 's|/download$||')
  if [ "$VERSION" = latest ]; then
    RELEASE_URL="$RELEASE_ROOT/latest/download"
  else
    case "$VERSION" in
      v*) TAG=$VERSION ;;
      *) TAG=v$VERSION ;;
    esac
    RELEASE_URL="$RELEASE_ROOT/download/$TAG"
  fi
  download "$TMP_DIR/$ARTIFACT_NAME" "$RELEASE_URL/$ARTIFACT_NAME"
  download "$TMP_DIR/SHA256SUMS" "$RELEASE_URL/SHA256SUMS"
fi
ARTIFACT="$TMP_DIR/$ARTIFACT_NAME"
SUMS="$TMP_DIR/SHA256SUMS"

verify_checksum() {
  expected=$(awk -v file="$ARTIFACT_NAME" 'length($1) == 64 && $1 ~ /^[[:xdigit:]]+$/ && ($2 == file || $2 == "*" file) { print $1; exit }' "$SUMS")
  [ -n "$expected" ] || die "SHA256SUMS has no entry for $ARTIFACT_NAME"
  if command -v sha256sum >/dev/null 2>&1; then
    actual=$(sha256sum "$ARTIFACT" | awk '{print $1}')
  elif command -v shasum >/dev/null 2>&1; then
    actual=$(shasum -a 256 "$ARTIFACT" | awk '{print $1}')
  else
    die "sha256sum or shasum is required"
  fi
  [ "$actual" = "$expected" ] || die "checksum mismatch for $ARTIFACT_NAME"
}
verify_checksum

ensure_user() {
  [ "$SKIP_USER" = 1 ] && return 0
  if ! getent group atalaya >/dev/null 2>&1; then groupadd --system atalaya; fi
  if ! getent passwd atalaya >/dev/null 2>&1; then
    useradd --system --gid atalaya --home-dir /var/lib/atalaya --shell /usr/sbin/nologin atalaya
  fi
}
ensure_dir() {
  mkdir -p "$1"
  chmod "$2" "$1"
  if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown atalaya:atalaya "$1"; fi
}
write_if_absent() {
  path=$1
  mode=$2
  content=$3
  if [ -e "$path" ]; then
    [ ! -L "$path" ] || die "existing path is a symlink: $path"
    [ -f "$path" ] || die "existing path is not a regular file: $path"
    chmod "$mode" "$path"
    if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown atalaya:atalaya "$path"; fi
    return 0
  fi
  temporary=$(dirname -- "$path")/.atalaya-install-$(basename -- "$path").$$
  (umask 077; printf '%s\n' "$content" > "$temporary")
  chmod "$mode" "$temporary"
  if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown atalaya:atalaya "$temporary"; fi
  if ! ln "$temporary" "$path" 2>/dev/null; then
    rm -f "$temporary"
    [ -f "$path" ] || die "could not install $path"
    return 0
  fi
  rm -f "$temporary"
}
ensure_master_key() {
  key_path="$MASTER_KEY"
  if [ -e "$key_path" ]; then
    [ ! -L "$key_path" ] || die "master key path is a symlink"
    [ -f "$key_path" ] || die "master key path is not regular"
    chmod 0600 "$key_path"
    if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown atalaya:atalaya "$key_path"; fi
    return 0
  fi
  key_dir=$(dirname -- "$key_path")
  mkdir -p "$key_dir"
  temporary="$key_dir/.atalaya-master-key.$$"
  (umask 077; dd if=/dev/urandom of="$temporary" bs=32 count=1 2>/dev/null)
  chmod 0600 "$temporary"
  if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown atalaya:atalaya "$temporary"; fi
  if ! ln "$temporary" "$key_path" 2>/dev/null; then
    rm -f "$temporary"
    [ -f "$key_path" ] || die "could not install master key"
  fi
  rm -f "$temporary"
}
yaml_value() {
  key=$1
  file=$2
  awk -v key="$key" '
    {
      line=$0
      sub("^[[:space:]]*", "", line)
      prefix=key ":"
      if (index(line, prefix) != 1) next
      value=substr(line, length(prefix) + 1)
      sub("^[[:space:]]+", "", value)
      sub("[[:space:]]+$", "", value)
      quote=sprintf("%c", 39)
      if (length(value) >= 2 && ((substr(value, 1, 1) == "\"" && substr(value, length(value), 1) == "\"") || (substr(value, 1, 1) == quote && substr(value, length(value), 1) == quote))) {
        value=substr(value, 2, length(value) - 2)
      }
      print value
      exit
    }
  ' "$file"
}
resolve_config_path() {
  config_path=$1
  value=$2
  case "$value" in
    /*) printf '%s\n' "$value" ;;
    *) printf '%s/%s\n' "$(dirname -- "$config_path")" "$value" ;;
  esac
}
load_server_paths() {
  [ -f "$SERVER_CONFIG" ] || die "server configuration is missing: $SERVER_CONFIG"
  listen=$(yaml_value listen "$SERVER_CONFIG")
  database_path=$(yaml_value database_path "$SERVER_CONFIG")
  master_key_path=$(yaml_value master_key_path "$SERVER_CONFIG")
  [ -n "$listen" ] || die "server configuration has no listen address"
  [ -n "$database_path" ] || die "server configuration has no database_path"
  [ -n "$master_key_path" ] || die "server configuration has no master_key_path"
  SERVER_LISTEN=$listen
  DATABASE=$(resolve_config_path "$SERVER_CONFIG" "$database_path")
  MASTER_KEY=$(resolve_config_path "$SERVER_CONFIG" "$master_key_path")
}
load_agent_paths() {
  [ -f "$AGENT_CONFIG" ] || die "agent configuration is missing: $AGENT_CONFIG"
  listen=$(yaml_value listen "$AGENT_CONFIG")
  credential_path=$(yaml_value credential_path "$AGENT_CONFIG")
  [ -n "$listen" ] || die "agent configuration has no listen address"
  [ -n "$credential_path" ] || die "agent configuration has no credential_path"
  AGENT_LISTEN=$listen
  AGENT_CREDENTIAL=$(resolve_config_path "$AGENT_CONFIG" "$credential_path")
}
ensure_runtime_files() {
  if [ "$SKIP_USER" = 1 ] || [ "$(id -u)" -ne 0 ]; then
    return 0
  fi
  for path in "$SERVER_CONFIG" "$AGENT_CONFIG" "$MASTER_KEY" "$DATABASE" "$DATABASE-wal" "$DATABASE-shm" "$DATABASE-journal" "$AGENT_CREDENTIAL"; do
    if [ -e "$path" ]; then
      [ ! -L "$path" ] || die "runtime file is a symlink: $path"
      [ -f "$path" ] || die "runtime file is not regular: $path"
      chown atalaya:atalaya "$path"
    fi
  done
}
get_dns_name() {
  status=$("$TAILSCALE" status --json 2>/dev/null) || return 1
  if command -v jq >/dev/null 2>&1; then
    printf '%s' "$status" | jq -r '.Self.DNSName // empty' | sed 's/\.$//'
  else
    printf '%s' "$status" | sed -n 's/.*"DNSName"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | sed 's/\.$//' | head -n 1
  fi
}
DNS_NAME=${ATALAYA_DNS_NAME:-}
[ -n "$DNS_NAME" ] || DNS_NAME=$(get_dns_name || true)
[ -n "$DNS_NAME" ] || die "Tailscale Self.DNSName is unavailable; no public fallback is permitted"

ensure_user
ensure_dir "$ETC_DIR" 0750
ensure_dir "$STATE_DIR" 0750
mkdir -p "$(dirname -- "$INSTALL_BIN")" "$SYSTEMD_DIR"

SERVER_CONFIG="$ETC_DIR/server.yaml"
AGENT_CONFIG="$ETC_DIR/agent.yaml"
MASTER_KEY="$ETC_DIR/master.key"
DATABASE="$STATE_DIR/atalaya.db"
AGENT_CREDENTIAL="$STATE_DIR/agent-credential"
SERVER_LISTEN=127.0.0.1:8080
AGENT_LISTEN=127.0.0.1:9474

if [ "$ROLE" = server ]; then
  write_if_absent "$SERVER_CONFIG" 0640 "listen: 127.0.0.1:8080
mode: production
public_url: https://$DNS_NAME
database_path: $DATABASE
master_key_path: $MASTER_KEY
trusted_proxy: tailscale_serve"
fi
if [ "$ROLE" = agent ] && [ ! -f "$AGENT_CONFIG" ]; then
  [ -n "$SERVER_URL" ] || die "--server-url or an existing agent.yaml is required for agent role"
  [ -n "$AGENT_URL" ] || AGENT_URL=https://$DNS_NAME:9474
  [ -n "$DISPLAY_NAME" ] || DISPLAY_NAME=local-agent
  write_if_absent "$AGENT_CONFIG" 0640 "listen: 127.0.0.1:9474
server_url: $SERVER_URL
advertised_url: $AGENT_URL
credential_path: $AGENT_CREDENTIAL"
fi
if [ "$ROLE" = server ] || [ "$ROLE" = upgrade ]; then
  if [ "$ROLE" = upgrade ]; then
    [ -f "$SERVER_CONFIG" ] || die "upgrade requires existing $SERVER_CONFIG"
  fi
  load_server_paths
  if [ -f "$AGENT_CONFIG" ]; then load_agent_paths; fi
  if [ "$ROLE" = upgrade ]; then
    [ -f "$MASTER_KEY" ] || die "upgrade requires existing $MASTER_KEY"
  fi
  ensure_master_key
fi
if [ "$ROLE" = agent ]; then load_agent_paths; fi
ensure_runtime_files

install_binary() {
  temporary="$INSTALL_BIN.new.$$"
  cp "$ARTIFACT" "$temporary"
  chmod 0755 "$temporary"
  if [ "$SKIP_USER" != 1 ] && [ "$(id -u)" -eq 0 ]; then chown root:root "$temporary"; fi
  mv -f "$temporary" "$INSTALL_BIN"
}
install_units() {
  server_unit="$TMP_DIR/atalaya-server.service"
  agent_unit="$TMP_DIR/atalaya-agent.service"
  # This installer is commonly piped to `sh`; never source units from $0's
  # working directory because it may be an untrusted checkout.
  cat > "$server_unit" <<'EOF'
[Unit]
Description=Atalaya server
Wants=network-online.target
After=network-online.target tailscaled.service
Requires=tailscaled.service

[Service]
Type=simple
User=atalaya
Group=atalaya
ExecStart=/usr/local/bin/atalaya server --config /etc/atalaya/server.yaml
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=/var/lib/atalaya
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0077

[Install]
WantedBy=multi-user.target
EOF
  cat > "$agent_unit" <<'EOF'
[Unit]
Description=Atalaya monitoring agent
Wants=network-online.target
After=network-online.target tailscaled.service
Requires=tailscaled.service

[Service]
Type=simple
User=atalaya
Group=atalaya
ExecStart=/usr/local/bin/atalaya agent --config /etc/atalaya/agent.yaml
Restart=on-failure
RestartSec=5s
NoNewPrivileges=yes
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
CapabilityBoundingSet=
AmbientCapabilities=
ReadWritePaths=/var/lib/atalaya
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0077

[Install]
WantedBy=multi-user.target
EOF
  install -m 0644 "$server_unit" "$SYSTEMD_DIR/atalaya-server.service"
  install -m 0644 "$agent_unit" "$SYSTEMD_DIR/atalaya-agent.service"
  "$SYSTEMCTL" daemon-reload
}
start_services() {
  case "$ROLE" in
    server) "$SYSTEMCTL" enable --now atalaya-agent.service atalaya-server.service ;;
    agent) "$SYSTEMCTL" enable --now atalaya-agent.service ;;
  esac
}
stop_services() {
  case "$ROLE" in
    server|upgrade) "$SYSTEMCTL" stop atalaya-server.service atalaya-agent.service ;;
    agent) "$SYSTEMCTL" stop atalaya-agent.service ;;
  esac
}
configure_serve() {
  case "$ROLE" in
    server|upgrade)
      "$INSTALL_BIN" tailscale serve --role server --server-config "$SERVER_CONFIG" --agent-config "$AGENT_CONFIG"
      ;;
    agent)
      "$INSTALL_BIN" tailscale serve --role agent --agent-config "$AGENT_CONFIG"
      ;;
  esac
}
healthcheck() {
  check_url() {
    path=$1
    if command -v curl >/dev/null 2>&1; then
      curl --fail --silent --show-error --max-time 10 "http://$path" >/dev/null
    elif command -v wget >/dev/null 2>&1; then
      wget --quiet --timeout=10 --output-document=- "http://$path" >/dev/null
    else
      die "curl or wget is required for health checks"
    fi
  }
  case "$ROLE" in
    server|upgrade)
      check_url "$SERVER_LISTEN/healthz"
      check_url "$AGENT_LISTEN/agent/v1/healthz"
      ;;
    agent)
      check_url "$AGENT_LISTEN/agent/v1/healthz"
      ;;
  esac
}
rollback_upgrade() {
  printf '%s\n' "atalaya install: upgrade failed; restoring previous binary and pre-migration database" >&2
  set +e
  "$SYSTEMCTL" stop atalaya-server.service atalaya-agent.service >/dev/null 2>&1
  rollback_binary="$INSTALL_BIN.rollback.$$"
  if ! cp -p "$OLD_BINARY" "$rollback_binary" || ! chmod 0755 "$rollback_binary" || ! mv -f "$rollback_binary" "$INSTALL_BIN"; then
    rm -f "$rollback_binary"
    printf '%s\n' "atalaya install: could not restore the previous binary; services remain stopped" >&2
    exit 1
  fi
  rollback_tmp=$(mktemp -d "$STATE_DIR/.atalaya-rollback.XXXXXX")
  database_restored=0
  if tar -xzf "$BACKUP_PATH" -C "$rollback_tmp" --no-same-owner --no-same-permissions >/dev/null 2>&1 && [ -f "$rollback_tmp/atalaya.db" ]; then
    if cp "$rollback_tmp/atalaya.db" "$DATABASE.rollback" &&
      chmod 0600 "$DATABASE.rollback" &&
      rm -f "$DATABASE-wal" "$DATABASE-shm" "$DATABASE-journal" &&
      mv -f "$DATABASE.rollback" "$DATABASE"; then
      database_restored=1
    fi
  fi
  rm -rf "$rollback_tmp"
  if [ "$database_restored" -ne 1 ]; then
    printf '%s\n' "atalaya install: could not restore the pre-migration database; services remain stopped" >&2
    exit 1
  fi
  ensure_runtime_files
  "$SYSTEMCTL" daemon-reload >/dev/null 2>&1
  if "$SYSTEMCTL" enable --now atalaya-agent.service atalaya-server.service >/dev/null 2>&1 &&
    healthcheck &&
    "$INSTALL_BIN" verify --config "$SERVER_CONFIG" >/dev/null 2>&1; then
    rm -f "$OLD_BINARY"
  else
    printf '%s\n' "atalaya install: rollback healthcheck failed; previous binary is retained at $OLD_BINARY" >&2
  fi
  exit 1
}

if [ "$ROLE" = upgrade ] && [ "$NO_START" -eq 0 ]; then
  [ -x "$INSTALL_BIN" ] || die "cannot upgrade without an installed binary"
  SERVER_CONFIG="$ETC_DIR/server.yaml"
  [ -f "$SERVER_CONFIG" ] || die "cannot upgrade without $SERVER_CONFIG"
  ensure_dir "$STATE_DIR/backups" 0750
  ensure_dir "$STATE_DIR/rollback" 0750
  STAMP=$(date -u +%Y%m%dT%H%M%SZ)
  BACKUP_PATH="$STATE_DIR/backups/pre-upgrade-$STAMP.tar.gz"
  OLD_BINARY="$STATE_DIR/rollback/atalaya-$STAMP"
  stop_services
  cp -p "$INSTALL_BIN" "$OLD_BINARY"
  if ! "$INSTALL_BIN" backup --config "$SERVER_CONFIG" --output "$BACKUP_PATH"; then
    "$SYSTEMCTL" enable --now atalaya-agent.service atalaya-server.service >/dev/null 2>&1 || true
    die "pre-migration backup failed; previous services were requested to restart"
  fi
  install_binary
  install_units
  if ! "$INSTALL_BIN" migrate --config "$SERVER_CONFIG"; then rollback_upgrade; fi
  if ! "$INSTALL_BIN" verify --config "$SERVER_CONFIG"; then rollback_upgrade; fi
  ensure_runtime_files
  if ! configure_serve; then rollback_upgrade; fi
  if ! start_services; then rollback_upgrade; fi
  if ! healthcheck; then rollback_upgrade; fi
  if ! "$INSTALL_BIN" verify --config "$SERVER_CONFIG" >/dev/null; then rollback_upgrade; fi
  rm -f "$OLD_BINARY"
  printf '%s\n' "atalaya upgrade completed"
  exit 0
fi

install_binary
install_units
if [ "$NO_START" -eq 1 ]; then
  printf '%s\n' "atalaya installed without starting or migrating services"
  exit 0
fi

if [ "$ROLE" = server ]; then
  "$INSTALL_BIN" migrate --config "$SERVER_CONFIG"
  [ -n "$AGENT_URL" ] || AGENT_URL=https://$DNS_NAME:9474
  [ -n "$DISPLAY_NAME" ] || DISPLAY_NAME=local-agent
  "$INSTALL_BIN" bootstrap local-agent --server-config "$SERVER_CONFIG" --agent-config "$AGENT_CONFIG" --agent-url "$AGENT_URL" --credential-path "$AGENT_CREDENTIAL" --display-name "$DISPLAY_NAME"
  verify_output=
  verify_status=0
  verify_output=$("$INSTALL_BIN" verify --config "$SERVER_CONFIG" 2>&1) || verify_status=$?
  if [ "$verify_status" -ne 0 ]; then
    printf '%s\n' "$verify_output" >&2
    die "verification failed after bootstrap"
  fi
  if ! printf '%s\n' "$verify_output" | grep -q 'administrator=present'; then
    if [ -t 0 ]; then
      "$INSTALL_BIN" admin create --config "$SERVER_CONFIG"
    else
      printf '%s\n' "atalaya install: no administrator exists; run atalaya admin create before login" >&2
    fi
  fi
  ensure_runtime_files
fi
configure_serve
start_services
printf '%s %s\n' "atalaya" "$ROLE installation completed"
