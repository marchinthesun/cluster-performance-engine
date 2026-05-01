#!/usr/bin/env bash
# Install the nexusflow binary as nexusflow + kubermetrics (same file, argv0 routing).
#   PREFIX=/usr/local ./install_bins.sh [path/to/built-nexusflow]
# From module dir: go build -o ./nexusflow ./cmd/nexusflow && ./install_bins.sh ./nexusflow

set -euo pipefail
PREFIX="${PREFIX:-/usr/local}"
EXE="${1:-./nexusflow}"
if [ ! -f "$EXE" ]; then
	echo "usage: PREFIX=/usr/local $0 [path/to/nexusflow]" >&2
	echo "  build: (cd \$(dirname \"\$0\") && go build -o ./nexusflow ./cmd/nexusflow)" >&2
	exit 2
fi
install -Dm755 "$EXE" "$PREFIX/bin/nexusflow"
install -Dm755 "$EXE" "$PREFIX/bin/kubermetrics"
if [ "${NEXUSFLOW_INSTALL_VERBOSE:-}" = "1" ]; then
	echo "installed $PREFIX/bin/nexusflow and $PREFIX/bin/kubermetrics"
fi
