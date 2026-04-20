#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out_dir="${OUT_DIR:-"$root/dist"}"

case "$out_dir" in
  "" | "/")
    printf 'refusing unsafe OUT_DIR: %s\n' "$out_dir" >&2
    exit 1
    ;;
esac

default_targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)

if [[ -n "${TARGETS:-}" ]]; then
  # shellcheck disable=SC2206
  targets=(${TARGETS})
else
  targets=("${default_targets[@]}")
fi

rm -rf "$out_dir"
mkdir -p "$out_dir"

asset_os() {
  case "$1" in
    darwin | linux) printf '%s' "$1" ;;
    windows) printf 'windows' ;;
    *) printf 'unsupported GOOS: %s\n' "$1" >&2; return 1 ;;
  esac
}

asset_ext() {
  case "$1" in
    windows) printf '.exe' ;;
    *) printf '' ;;
  esac
}

build_one() {
  local command="$1"
  local goos="$2"
  local goarch="$3"
  local os_name ext asset

  os_name="$(asset_os "$goos")"
  ext="$(asset_ext "$goos")"
  asset="${command}_${os_name}_${goarch}${ext}"

  printf 'building %s for %s/%s\n' "$command" "$goos" "$goarch"
  CGO_ENABLED="${CGO_ENABLED:-0}" GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags="-s -w" -o "$out_dir/$asset" "./cmd/$command"
  if [[ "$goos" != "windows" ]]; then
    chmod 0755 "$out_dir/$asset"
  fi
}

cd "$root"
for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  build_one "mdpp" "$goos" "$goarch"
  build_one "mdpp-lsp" "$goos" "$goarch"
done

(
  cd "$out_dir"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum mdpp* > checksums.txt
  else
    shasum -a 256 mdpp* > checksums.txt
  fi
)

printf 'release artifacts written to %s\n' "$out_dir"
