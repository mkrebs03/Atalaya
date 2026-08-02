#!/bin/sh
set -eu

SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TEST_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/atalaya-install-test.XXXXXX")
trap 'rm -rf "$TEST_ROOT"' EXIT HUP INT TERM

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) printf '%s\n' "unsupported test architecture" >&2; exit 1 ;;
esac

RELEASE_ROOT="$TEST_ROOT/releases"
ARTIFACT_ROOT="$RELEASE_ROOT/download/dev"
mkdir -p "$ARTIFACT_ROOT"
write_checksum() {
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$ARTIFACT_ROOT" && sha256sum "atalaya-linux-$ARCH" > SHA256SUMS)
  else
    (cd "$ARTIFACT_ROOT" && shasum -a 256 "atalaya-linux-$ARCH" > SHA256SUMS)
  fi
}
TRUE_BINARY=
for candidate in /usr/bin/true /bin/true; do
  if [ -x "$candidate" ]; then
    TRUE_BINARY=$candidate
    break
  fi
done
[ -n "$TRUE_BINARY" ] || { printf '%s\n' "could not find an external true binary" >&2; exit 1; }
cp "$TRUE_BINARY" "$ARTIFACT_ROOT/atalaya-linux-$ARCH"
write_checksum

if ATALAYA_ROOT="$TEST_ROOT/root" \
  ATALAYA_DNS_NAME=oracle.example.ts.net \
  ATALAYA_SKIP_SYSTEM_USER=1 \
  ATALAYA_SYSTEMCTL=true \
  sh "$SCRIPT_DIR/install.sh" server \
    --channel dev \
    --base-url "file://$RELEASE_ROOT" \
    --no-start; then
  :
else
  printf '%s\n' "dev channel installation failed" >&2
  exit 1
fi

test -x "$TEST_ROOT/root/usr/local/bin/atalaya"
test -f "$TEST_ROOT/root/etc/atalaya/server.yaml"
test -f "$TEST_ROOT/root/etc/systemd/system/atalaya-server.service"
test -f "$TEST_ROOT/root/etc/systemd/system/atalaya-agent.service"

FAKE_BIN_DIR="$TEST_ROOT/fake-bin"
FAKE_BINARY="$TEST_ROOT/atalaya-fake"
FAKE_SYSTEMCTL="$TEST_ROOT/systemctl-fake"
SYSTEMCTL_LOG="$TEST_ROOT/systemctl.log"
mkdir -p "$FAKE_BIN_DIR"
cat > "$FAKE_BINARY" <<'EOF'
#!/bin/sh
set -eu

command=${1:-}
shift || true
case "$command" in
  backup)
    output=
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --output)
          output=$2
          shift 2
          ;;
        *)
          shift
          ;;
      esac
    done
    [ -n "$output" ]
    temporary=$(mktemp -d "${TMPDIR:-/tmp}/atalaya-fake-backup.XXXXXX")
    printf '%s\n' fake-database > "$temporary/atalaya.db"
    tar -czf "$output" -C "$temporary" atalaya.db
    rm -rf "$temporary"
    ;;
  migrate|tailscale|verify)
    ;;
  *)
    ;;
esac
EOF
chmod 0755 "$FAKE_BINARY"
# The initial install intentionally uses the system `true` binary to verify that
# --no-start does not invoke application commands. Before exercising upgrade,
# seed the installed path with a previous Atalaya-like binary that can create
# the pre-migration backup expected from a real upgrade.
cp "$FAKE_BINARY" "$TEST_ROOT/root/usr/local/bin/atalaya"
cat > "$FAKE_SYSTEMCTL" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$ATALAYA_SYSTEMCTL_LOG"
EOF
chmod 0755 "$FAKE_SYSTEMCTL"
cat > "$FAKE_BIN_DIR/curl" <<'EOF'
#!/bin/sh
exit 0
EOF
chmod 0755 "$FAKE_BIN_DIR/curl"
cp "$FAKE_BINARY" "$ARTIFACT_ROOT/atalaya-linux-$ARCH"
write_checksum

if ATALAYA_ROOT="$TEST_ROOT/root" \
  ATALAYA_DNS_NAME=oracle.example.ts.net \
  ATALAYA_SKIP_SYSTEM_USER=1 \
  ATALAYA_SYSTEMCTL="$FAKE_SYSTEMCTL" \
  ATALAYA_SYSTEMCTL_LOG="$SYSTEMCTL_LOG" \
  PATH="$FAKE_BIN_DIR:$PATH" \
  sh "$SCRIPT_DIR/install.sh" upgrade \
    --channel dev \
    --base-url "file://$RELEASE_ROOT"; then
  :
else
  printf '%s\n' "upgrade installation failed" >&2
  exit 1
fi

grep -Fqx 'enable --now atalaya-agent.service atalaya-server.service' "$SYSTEMCTL_LOG"
test -n "$(find "$TEST_ROOT/root/var/lib/atalaya/backups" -type f -name 'pre-upgrade-*.tar.gz' -print -quit)"
cmp "$FAKE_BINARY" "$TEST_ROOT/root/usr/local/bin/atalaya"

if ATALAYA_ROOT="$TEST_ROOT/conflict" \
  ATALAYA_DNS_NAME=oracle.example.ts.net \
  ATALAYA_SKIP_SYSTEM_USER=1 \
  sh "$SCRIPT_DIR/install.sh" server --channel dev --version v0.0.1 >/dev/null 2>&1; then
  printf '%s\n' "--channel and --version should conflict" >&2
  exit 1
fi

if ATALAYA_ROOT="$TEST_ROOT/version-channel-conflict" \
  ATALAYA_DNS_NAME=oracle.example.ts.net \
  ATALAYA_SKIP_SYSTEM_USER=1 \
  sh "$SCRIPT_DIR/install.sh" server --version dev >/dev/null 2>&1; then
  printf '%s\n' "--version dev should require --channel dev" >&2
  exit 1
fi
