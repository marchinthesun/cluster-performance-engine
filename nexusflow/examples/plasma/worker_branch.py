#!/usr/bin/env python3
"""Dynamic branch: mmap shared memory via SCM_RIGHTS, sample perf fd, spawn a new DAG node."""

from __future__ import annotations

import os
import sys

from nexusflow_sdk.plasma import PlasmaClient, mmap_shm_fd, perf_window_count


def main() -> None:
    sock = os.environ.get("NEXUSFLOW_PLASMA_SOCK")
    if not sock:
        print("worker_branch: missing NEXUSFLOW_PLASMA_SOCK", file=sys.stderr)
        sys.exit(2)
    node_id = os.environ.get("NEXUSFLOW_NODE_ID", "bootstrap")

    with PlasmaClient(sock) as cl:
        print(cl.hello(client="worker_branch"))

        meta, shm_fd = cl.request_fd("shm")
        try:
            size = int(meta["shm_size"])
            mm = mmap_shm_fd(shm_fd, size)
            mm[:8] = b"PLASMA\x01\x02"
            mm.flush()
            print(f"magic wrote to shm path={meta.get('shm_path')!r}")
        finally:
            os.close(shm_fd)

        cycles = None
        try:
            meta_p, pfd = cl.request_fd("perf_cycles")
            try:
                cycles = perf_window_count(pfd, meta_p, sleep_ms=50)
                print(f"perf_cycles sample window={cycles}")
            finally:
                os.close(pfd)
        except (RuntimeError, OSError, ValueError) as ex:
            print(f"perf_cycles unavailable ({ex}); need CAP_SYS_ADMIN/CAP_PERFMON or privileged for perf_event_open.")

        leaf = {
            "id": "leaf_dynamic",
            "cmd": ["python3", "/demo/worker_leaf.py"],
            "cpus": 0,
            "depends_on": [node_id],
        }
        print("branch:", cl.branch([leaf], parent=node_id))

        kw = {"node_id": node_id}
        if cycles is not None:
            kw["cycles"] = cycles
        print(cl.send_sample(**kw))


if __name__ == "__main__":
    main()
