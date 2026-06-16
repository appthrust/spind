#!/usr/bin/env bash
set -euo pipefail

template_dir="${SPIND_TEMPLATE_DIR:-/work/template}"
build_dir="${SPIND_BUILD_DIR:-/work/build}"
output_dir="${SPIND_OUTPUT_DIR:-/work/output/image}"
output_parent="$(dirname "$output_dir")"
guest_binary_dir="${SPIND_GUEST_BINARY_DIR:-$template_dir/guest-binaries}"
guest_agent_path="$guest_binary_dir/spind-guest-agent"

restore_host_ownership() {
  if [ -z "${SPIND_HOST_UID:-}" ] || [ -z "${SPIND_HOST_GID:-}" ]; then
    return
  fi
  for path in "$template_dir" "$build_dir" "$output_parent"; do
    case "$path" in
      ""|"/")
        continue
        ;;
    esac
    if [ -e "$path" ]; then
      chown -R "$SPIND_HOST_UID:$SPIND_HOST_GID" "$path" 2>/dev/null || true
      chmod -R u+rwX "$path" 2>/dev/null || true
    fi
  done
}

guest_agent_goarch() {
  case "$system" in
    x86_64-linux)
      printf '%s\n' amd64
      ;;
    aarch64-linux)
      printf '%s\n' arm64
      ;;
    *)
      echo "unsupported Nix system for spind guest agent: $system" >&2
      exit 1
      ;;
  esac
}

prepare_guest_agent() {
  if [ -x "$guest_agent_path" ]; then
    return
  fi

  package="${SPIND_GUEST_AGENT_PACKAGE:-github.com/suin/spind/cmd/agent}"
  version="${SPIND_GUEST_AGENT_VERSION:-latest}"
  case "$version" in
    ""|"(devel)")
      version="latest"
      ;;
  esac

  goarch="$(guest_agent_goarch)"
  go_bin_dir="$build_dir/go-bin"
  go_cache_dir="$build_dir/go-cache"
  go_mod_cache_dir="$build_dir/go-mod-cache"
  rm -rf "$go_bin_dir"
  mkdir -p "$guest_binary_dir" "$go_bin_dir" "$go_cache_dir" "$go_mod_cache_dir"

  echo "installing spind guest agent ${package}@${version} for linux/${goarch}" >&2
  GOBIN="$go_bin_dir" \
    GOCACHE="$go_cache_dir" \
    GOMODCACHE="$go_mod_cache_dir" \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH="$goarch" \
    go install "${package}@${version}"

  if [ ! -f "$go_bin_dir/agent" ]; then
    echo "go install did not produce $go_bin_dir/agent" >&2
    exit 1
  fi
  install -m 0755 "$go_bin_dir/agent" "$guest_agent_path"
}

trap restore_host_ownership EXIT

system="${SPIND_NIX_SYSTEM:-$(nix --extra-experimental-features 'nix-command flakes' eval --impure --raw --expr builtins.currentSystem)}"

rm -rf "$build_dir/result" "$output_dir"
mkdir -p "$build_dir" "$output_dir"

prepare_guest_agent

cd "$template_dir"
nix --extra-experimental-features 'nix-command flakes' build --accept-flake-config \
  ".#packages.${system}.default" \
  --out-link "$build_dir/result"

cp -aL "$build_dir/result/." "$output_dir/"

metadata="$output_dir/metadata.json"
if [ ! -f "$metadata" ]; then
  echo "built image missing metadata.json" >&2
  exit 1
fi

jq -c '.disks[]? | select(.create != null)' "$metadata" | while IFS= read -r disk_json; do
  name="$(printf '%s' "$disk_json" | jq -r '.name')"
  case "$name" in
    ""|/*|*/*)
      echo "invalid data disk name: $name" >&2
      exit 1
      ;;
  esac

  size="${SPIND_DATA_SIZE:-$(printf '%s' "$disk_json" | jq -r '.create.size // "8GiB"')}"
  fs_type="$(printf '%s' "$disk_json" | jq -r '.create.fsType // "ext4"')"
  label="$(printf '%s' "$disk_json" | jq -r '.create.label // empty')"
  path="$output_dir/$name"

  rm -f "$path"
  truncate -s "$size" "$path"

  args=(-q -t "$fs_type")
  if [ -n "$label" ]; then
    args+=(-L "$label")
  fi
  args+=("$path")
  mke2fs "${args[@]}"
done

restore_host_ownership
trap - EXIT
