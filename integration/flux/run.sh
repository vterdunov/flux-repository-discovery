#!/usr/bin/env bash
set -euo pipefail

# Run from anywhere with: mise exec -- bash integration/flux/run.sh
# No kubeconfig or existing cluster is used. Only an ephemeral local envtest
# API server is allowed, including when USE_EXISTING_CLUSTER is set by a caller.
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cache_dir="$project_root/.cache"
upstream_dir="$cache_dir/flux-operator"
flux_version="v0.59.0"
flux_commit="e0f71db839116724c84caf8e06ab8423e7d9b174"
kubernetes_version="1.36.2"
platform="$(go env GOOS)-$(go env GOARCH)"
assets_dir="$cache_dir/envtest-v$kubernetes_version"
archive="$cache_dir/envtest-v$kubernetes_version-$platform.tar.gz"

case "$platform" in
  darwin-arm64) checksum="9278f9e5af556b2f1f2d139769c1f0d717c7b4426917fdebdba898bcb725a916e4910d6160886194ff9be9589ea7c5c32c2b8ae0754703874b7c8ba8ddfc41ce" ;;
  darwin-amd64) checksum="b4fe9f973cd1992e3880f8b230ed32c0c243b83371591e3e3315f4404df2cdbef8ee94eeb9bb245b7e5feb987d5c4a76c43e0877654ab73f00d7bbc2b9984514" ;;
  linux-amd64) checksum="ea743186c8a799f5cf8faf16969f86189d003cb7d130e0ac4b58789f1e5748dcf30ebe91c837a10d5ac415383da3e10b9e64d65785c938c23e739781cfb76f08" ;;
  linux-arm64) checksum="2d72ee985a8e262a3c57dc9f7f0fd891f6a8c7bf7ebaa2db6dc6d8eac8ae28181afe51c1f368b67756cdb40b10de9b205609e1726e5f27f7c6d824dd9c6649ac" ;;
  *) echo "Unsupported envtest platform: $platform" >&2; exit 1 ;;
esac

mkdir -p "$cache_dir" "$assets_dir"
if [[ ! -d "$upstream_dir/.git" ]]; then
  git clone --depth 1 --branch "$flux_version" \
    https://github.com/controlplaneio-fluxcd/flux-operator.git "$upstream_dir"
fi
if [[ "$(git -C "$upstream_dir" rev-parse HEAD)" != "$flux_commit" ]]; then
  echo "Unexpected Flux commit in $upstream_dir; expected $flux_commit" >&2
  exit 1
fi
if [[ -n "$(git -C "$upstream_dir" status --porcelain --untracked-files=no)" ]]; then
  echo "Tracked files in $upstream_dir have changed; refusing an unverified Flux build" >&2
  exit 1
fi
if [[ ! -f "$archive" ]]; then
  curl --fail --location --silent --show-error \
    "https://github.com/kubernetes-sigs/controller-tools/releases/download/envtest-v$kubernetes_version/envtest-v$kubernetes_version-$platform.tar.gz" \
    --output "$archive"
fi
python3 - "$archive" "$checksum" <<'PY'
import hashlib
import pathlib
import sys

path = pathlib.Path(sys.argv[1])
actual = hashlib.sha512(path.read_bytes()).hexdigest()
if actual != sys.argv[2]:
    sys.exit(f"envtest SHA512 mismatch: {path}")
PY
if [[ ! -x "$assets_dir/controller-tools/envtest/kube-apiserver" ]]; then
  tar -xzf "$archive" -C "$assets_dir"
fi

overlay="$cache_dir/flux-contract-overlay.json"
python3 - "$project_root" "$overlay" <<'PY'
import json
import pathlib
import sys

root = pathlib.Path(sys.argv[1])
overlay = {
    "Replace": {
        str(root / ".cache/flux-operator/internal/controller/frd_contract_test.go"):
            str(root / "integration/flux/testdata/contract_test.go")
    }
}
pathlib.Path(sys.argv[2]).write_text(json.dumps(overlay))
PY

export USE_EXISTING_CLUSTER=false
export KUBEBUILDER_ASSETS="$assets_dir/controller-tools/envtest"
export KUBECONFIG="$cache_dir/frd-contract-no-existing-kubeconfig"
export GOCACHE="$cache_dir/go-build"
export GOMODCACHE="$cache_dir/go-mod"
unset KUBERNETES_SERVICE_HOST KUBERNETES_SERVICE_PORT
export FRD_CONTRACT_BRIDGE="$cache_dir/flux-contract-bridge"
cd "$project_root"
go build -race -tags=fluxcontract -o "$FRD_CONTRACT_BRIDGE" ./integration/flux/bridge
cd "$upstream_dir"
go test -count=1 -timeout=5m -tags=fluxcontract -overlay="$overlay" \
  ./internal/controller -run '^TestFRDExternalServiceContract$' -v \
  2>&1 | tee "$cache_dir/flux-contract-result.log"
