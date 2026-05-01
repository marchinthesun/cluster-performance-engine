"""NexusFlow Python SDK — topology discovery (sysfs + optional CLI JSON)."""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any
import json
import os
import subprocess


@dataclass
class Topology:
    """In-memory topology aligned with Go Snapshot JSON."""

    raw: dict[str, Any]

    @classmethod
    def from_json(cls, data: bytes | str) -> "Topology":
        if isinstance(data, bytes):
            payload = json.loads(data.decode())
        else:
            payload = json.loads(data)
        return cls(raw=payload)

    def dumps_indent(self) -> str:
        return json.dumps(self.raw, indent=2)


def discover_sysfs_linux() -> Topology:
    """Best-effort sysfs discovery (Linux only). Matches Go sysfs layout."""
    present_path = "/sys/devices/system/cpu/present"
    try:
        present_raw = open(present_path, encoding="utf-8").read().strip()
    except OSError as e:
        raise RuntimeError(f"nexusflow sysfs: cannot read {present_path}: {e}") from e

    cpu_ids = _parse_cpulist(present_raw)
    cpus: list[dict[str, Any]] = []
    numa_nodes: dict[int, list[int]] = {}

    node_root = "/sys/devices/system/node"
    try:
        for ent in sorted(os.listdir(node_root)):
            if not ent.startswith("node"):
                continue
            nid_str = ent[len("node") :]
            try:
                nid = int(nid_str)
            except ValueError:
                continue
            cpulist_path = os.path.join(node_root, ent, "cpulist")
            try:
                raw_list = open(cpulist_path, encoding="utf-8").read().strip()
            except OSError:
                continue
            lst = _parse_cpulist(raw_list)
            numa_nodes[nid] = lst
    except FileNotFoundError:
        numa_nodes = {}

    cpu_numa = {}
    for nid, lst in numa_nodes.items():
        for cid in lst:
            cpu_numa[cid] = nid

    for cid in cpu_ids:
        topo = f"/sys/devices/system/cpu/cpu{cid}/topology"
        pkg = _read_int(os.path.join(topo, "physical_package_id"), 0)
        core = _read_int(os.path.join(topo, "core_id"), cid)
        sib_raw = ""
        try:
            sib_raw = open(os.path.join(topo, "thread_siblings_list"), encoding="utf-8").read().strip()
        except OSError:
            pass
        siblings = _parse_cpulist(sib_raw) if sib_raw else [cid]
        cpus.append(
            {
                "id": cid,
                "package": pkg,
                "core": core,
                "numa_node": cpu_numa.get(cid, -1),
                "thread_siblings": siblings,
            }
        )

    snap = {
        "cpus": cpus,
        "numa_nodes": [{"id": nid, "cpus": lst} for nid, lst in sorted(numa_nodes.items())],
        "source": "sysfs-python",
    }
    return Topology(raw=snap)


def discover_go_cli(binary: str | None = None) -> Topology:
    """Invoke `nexusflow topology --json` (same schema as Go SDK)."""
    exe = binary or os.environ.get("NEXUSFLOW_BIN", "nexusflow")
    proc = subprocess.run(
        [exe, "topology", "--json"],
        check=False,
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(proc.stderr.strip() or "nexusflow topology failed")
    return Topology.from_json(proc.stdout)


def discover(prefer_cli: bool = False, cli_binary: str | None = None) -> Topology:
    """prefer_cli=True tries Go CLI first; else sysfs on Linux."""
    if prefer_cli:
        try:
            return discover_go_cli(cli_binary)
        except Exception:
            pass
    return discover_sysfs_linux()


def _read_int(path: str, default: int) -> int:
    try:
        return int(open(path, encoding="utf-8").read().strip())
    except Exception:
        return default


def _parse_cpulist(s: str) -> list[int]:
    s = s.strip()
    if not s:
        return []
    out: list[int] = []
    for part in s.split(","):
        part = part.strip()
        if not part:
            continue
        if "-" in part:
            a, _, b = part.partition("-")
            lo, hi = int(a.strip()), int(b.strip())
            out.extend(range(lo, hi + 1))
        else:
            out.append(int(part))
    return out
