#!/usr/bin/env bash
set -euo pipefail

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
installer="$repo_root/scripts/install.sh"
if [[ ! -f "$installer" ]]; then
  echo "missing installer: $installer" >&2
  exit 1
fi

case "$(uname -s)" in
  Darwin) goos=darwin ;;
  Linux) goos=linux ;;
  *) echo "unsupported test host: $(uname -s)" >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64|amd64) goarch=amd64 ;;
  arm64|aarch64) goarch=arm64 ;;
  *) echo "unsupported test architecture: $(uname -m)" >&2; exit 1 ;;
esac

version=v9.8.7
commit=abcdef1234567890
identity="agentred $version (abcdef1)"
asset="agentred-$version-$goos-$goarch.tar.gz"
tmp=$(mktemp -d)
server_pid=
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT

mkdir -p "$tmp/release"
(
  cd "$repo_root"
  make agentred-package \
    VERSION="$version" \
    COMMIT_ID="$commit" \
    AGENTRED_GOOS="$goos" \
    AGENTRED_GOARCH="$goarch" \
    AGENTRED_DIST_DIR="$tmp/release"
)
if command -v sha256sum >/dev/null 2>&1; then
  digest=$(sha256sum "$tmp/release/$asset" | awk '{print $1}')
else
  digest=$(shasum -a 256 "$tmp/release/$asset" | awk '{print $1}')
fi
printf '%s  %s\n' "$digest" "$asset" > "$tmp/release/SHA256SUMS"

port=$(python3 - <<'PY'
import socket
with socket.socket() as s:
    s.bind(('127.0.0.1', 0))
    print(s.getsockname()[1])
PY
)
python3 -m http.server "$port" --bind 127.0.0.1 --directory "$tmp/release" >"$tmp/http.log" 2>&1 &
server_pid=$!
base_url="http://127.0.0.1:$port"
for _ in $(seq 1 50); do
  if curl -fsS "$base_url/SHA256SUMS" >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done
curl -fsS "$base_url/SHA256SUMS" >/dev/null

explicit_dir="$tmp/explicit-bin"
explicit_output=$(AGENTRED_RELEASE_BASE_URL="$base_url" AGENTRED_INSTALL_DIR="$explicit_dir" sh "$installer")
[[ -x "$explicit_dir/agentred" ]]
[[ "$($explicit_dir/agentred --version)" == "$identity" ]]
grep -F "$identity" <<<"$explicit_output" >/dev/null

home_dir="$tmp/home"
unwritable_candidate="$tmp/not-a-directory"
printf 'occupied' > "$unwritable_candidate"
fallback_output=$(HOME="$home_dir" AGENTRED_RELEASE_BASE_URL="$base_url" \
  AGENTRED_SYSTEM_INSTALL_DIR="$unwritable_candidate" sh "$installer")
fallback_binary="$home_dir/.local/bin/agentred"
[[ -x "$fallback_binary" ]]
[[ "$($fallback_binary --version)" == "$identity" ]]
grep -F "$home_dir/.local/bin" <<<"$fallback_output" >/dev/null

printf '%064d  %s\n' 0 "$asset" > "$tmp/release/SHA256SUMS"
mismatch_dir="$tmp/mismatch-bin"
if AGENTRED_RELEASE_BASE_URL="$base_url" AGENTRED_INSTALL_DIR="$mismatch_dir" sh "$installer" >"$tmp/mismatch.out" 2>&1; then
  echo "installer accepted a checksum mismatch" >&2
  exit 1
fi
[[ ! -e "$mismatch_dir/agentred" ]]
grep -Ei 'checksum|sha-?256' "$tmp/mismatch.out" >/dev/null

echo "install.sh fixture tests passed"
