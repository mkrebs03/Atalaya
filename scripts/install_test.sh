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
TRUE_BINARY=
for candidate in /usr/bin/true /bin/true; do
  if [ -x "$candidate" ]; then
    TRUE_BINARY=$candidate
    break
  fi
done
[ -n "$TRUE_BINARY" ] || { printf '%s\n' "could not find an external true binary" >&2; exit 1; }
cp "$TRUE_BINARY" "$ARTIFACT_ROOT/atalaya-linux-$ARCH"
if command -v sha256sum >/dev/null 2>&1; then
  (cd "$ARTIFACT_ROOT" && sha256sum "atalaya-linux-$ARCH" > SHA256SUMS)
else
  (cd "$ARTIFACT_ROOT" && shasum -a 256 "atalaya-linux-$ARCH" > SHA256SUMS)
fi

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
