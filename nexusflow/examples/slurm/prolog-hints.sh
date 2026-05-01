#!/usr/bin/env bash
# Example fragment — paste into site-controlled JobProlog or user ~/.bash_profile on compute nodes.
# NexusFlow prints POSIX exports derived from THIS node's topology (once per job start).
set -euo pipefail
if command -v nexusflow >/dev/null 2>&1; then
  eval "$(nexusflow topology hints --format shell --source auto)"
fi
