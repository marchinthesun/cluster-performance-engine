#!/usr/bin/env bash
# Repo root: build image, optional local bin install, kubermetrics apply, optional dashboard UI.
#
#   ./install.sh
#   NEXUSFLOW_SKIP_BUILD=1 ./install.sh [--dry-run]
#   docker build -t nexusflow:local -f nexusflow/Dockerfile nexusflow && NEXUSFLOW_SKIP_BUILD=1 ./install.sh
#
# PREFIX                 default $HOME/.local (extracted binary install)
# NEXUSFLOW_IMAGE        image tag (default nexusflow:local)
# NEXUSFLOW_SDK_IDENTITY optional, passed into kubermetrics container
# NEXUSFLOW_KUBECONFIG   else KUBECONFIG or ~/.kube/config
# NEXUSFLOW_DOCKER_NETWORK  Linux: host (default) | bridge — kubermetrics step only
# NEXUSFLOW_UI           0 — skip UI after apply
# NEXUSFLOW_UI_PORT      host port (default 9842)
# NEXUSFLOW_UI_NAME      UI container name (default nexusflow-ui)
# NEXUSFLOW_UI_ATTACH    1 — foreground UI container
# NEXUSFLOW_INSTALL_VERBOSE   set to 1 for step-by-step install.sh messages and full kubermetrics logs

set -euo pipefail

nf_verbose() { [ "${NEXUSFLOW_INSTALL_VERBOSE:-}" = "1" ]; }
nf_log() { nf_verbose && printf '%s\n' "$*" || true; }

ROOT=$(CDPATH= cd -- "$(dirname "${BASH_SOURCE[0]}")" && pwd)
IMG="${NEXUSFLOW_IMAGE:-nexusflow:local}"
DOCKERFILE="$ROOT/nexusflow/Dockerfile"
CTX="$ROOT/nexusflow"

if [ -z "${NEXUSFLOW_SKIP_BUILD:-}" ]; then
	nf_log "install.sh: docker build -t $IMG"
	if nf_verbose; then
		docker build -t "$IMG" -f "$DOCKERFILE" "$CTX"
	else
		docker build -q -t "$IMG" -f "$DOCKERFILE" "$CTX"
	fi
else
	nf_log "install.sh: skipping docker build (NEXUSFLOW_SKIP_BUILD=$NEXUSFLOW_SKIP_BUILD)"
fi

TMPBIN="$ROOT/.nexusflow-extract-$$"
cleanup() {
	rm -f "$TMPBIN"
}
trap cleanup EXIT

nf_log "install.sh: extract binary from image"
CID=$(docker create "$IMG")
docker cp "$CID:/usr/local/bin/nexusflow" "$TMPBIN"
docker rm "$CID" >/dev/null
chmod +x "$TMPBIN"

PREFIX="${PREFIX:-$HOME/.local}"
export PREFIX
if [ ! -x "$ROOT/nexusflow/install_bins.sh" ]; then
	chmod +x "$ROOT/nexusflow/install_bins.sh" || true
fi
export NEXUSFLOW_INSTALL_VERBOSE="${NEXUSFLOW_INSTALL_VERBOSE:-}"
"$ROOT/nexusflow/install_bins.sh" "$TMPBIN"

cfg="${NEXUSFLOW_KUBECONFIG:-${KUBECONFIG:-$HOME/.kube/config}}"
opts="-v ${cfg}:/kube/config:ro"
if [ -d "${HOME}/.minikube" ]; then
	opts="$opts -v ${HOME}/.minikube:${HOME}/.minikube:ro"
fi

net=""
if [ "$(uname -s)" = "Linux" ]; then
	case "${NEXUSFLOW_DOCKER_NETWORK:-host}" in
	bridge | false | 0) ;;
	*)
		net="--network ${NEXUSFLOW_DOCKER_NETWORK:-host}"
		;;
	esac
fi

envopt=()
if [ -n "${NEXUSFLOW_SDK_IDENTITY:-}" ]; then
	envopt+=(-e "NEXUSFLOW_SDK_IDENTITY=$NEXUSFLOW_SDK_IDENTITY")
fi

skip_ui=false
for a in "$@"; do
	case "$a" in
	--dry-run | -dry-run | --print-id | -print-id)
		skip_ui=true
		;;
	esac
done

docker run --rm $net $opts "${envopt[@]}" "$IMG" /usr/local/bin/kubermetrics "$@"

if "$skip_ui" || [ "${NEXUSFLOW_UI:-1}" = "0" ]; then
	nf_log "install.sh: UI skipped (kubermetrics only / NEXUSFLOW_UI=0)"
	exit 0
fi

UIPORT="${NEXUSFLOW_UI_PORT:-9842}"
uiname="${NEXUSFLOW_UI_NAME:-nexusflow-ui}"
docker rm -f "$uiname" 2>/dev/null || true

if [ "${NEXUSFLOW_UI_ATTACH:-0}" = "1" ]; then
	echo "install.sh: UI at http://127.0.0.1:${UIPORT}/ (Ctrl+C stops container)"
	exec docker run --rm --name "$uiname" -p "${UIPORT}:9842" $opts "${envopt[@]}" -e SKIP_KUBE_DEPLOY=1 "$IMG"
fi

echo "install.sh: UI in background at http://127.0.0.1:${UIPORT}/"
docker run -d --rm --name "$uiname" -p "${UIPORT}:9842" $opts "${envopt[@]}" -e SKIP_KUBE_DEPLOY=1 "$IMG"
nf_log "install.sh: container $uiname | logs: docker logs -f $uiname | stop: docker stop $uiname"
