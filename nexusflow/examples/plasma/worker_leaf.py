#!/usr/bin/env python3
"""Runs after dynamic branch; verifies shm magic and emits another sample."""

from __future__ import annotations

import os
import sys

from nexusflow_sdk.plasma import PlasmaClient, mmap_shm_fd


def main() -> None:
    sock = os.environ.get("NEXUSFLOW_PLASMA_SOCK")
    if not sock:
        print("worker_leaf: missing NEXUSFLOW_PLASMA_SOCK", file=sys.stderr)
        sys.exit(2)
    node_id = os.environ.get("NEXUSFLOW_NODE_ID", "leaf_dynamic")

    shm_path = os.environ.get("NEXUSFLOW_SHM_PATH")
    shm_size = int(os.environ.get("NEXUSFLOW_SHM_SIZE", "0"))

    with PlasmaClient(sock) as cl:
        print(cl.hello(client="worker_leaf"))
        meta, fd = cl.request_fd("shm")
        try:
            mm = mmap_shm_fd(fd, int(meta["shm_size"]))
            magic = mm[:8]
            print(f"leaf sees shm magic={magic!r} env_path={shm_path!r}")
        finally:
            os.close(fd)

        print(cl.send_sample(node_id=node_id, cycles=shm_size & 0xFFFF))


if __name__ == "__main__":
    main()
