"""Unix-socket Plasma client: framed JSON + SCM_RIGHTS FD receipt (Linux)."""

from __future__ import annotations

import json
import mmap
import os
import socket
import struct
import time
from typing import Any

_MAX_FRAME = 4 << 20
_SOL_SOCKET = socket.SOL_SOCKET
_SCM_RIGHTS = getattr(socket, "SCM_RIGHTS", 1)


class PlasmaClient:
    def __init__(self, sock_path: str):
        self._path = sock_path
        self._sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        self._sock.connect(sock_path)

    def close(self) -> None:
        self._sock.close()

    def __enter__(self) -> PlasmaClient:
        return self

    def __exit__(self, *args: object) -> None:
        self.close()

    def _send_frame(self, obj: dict[str, Any]) -> None:
        raw = json.dumps(obj, separators=(",", ":")).encode("utf-8")
        if len(raw) > _MAX_FRAME:
            raise ValueError("payload too large")
        self._sock.sendall(struct.pack(">I", len(raw)) + raw)

    def _recv_frame_json(self) -> dict[str, Any]:
        hdr = self._recv_exact(4)
        ln = struct.unpack(">I", hdr)[0]
        if ln > _MAX_FRAME:
            raise ValueError("frame too large")
        body = self._recv_exact(ln) if ln else b""
        return json.loads(body.decode("utf-8"))

    def _recv_exact(self, n: int) -> bytes:
        parts: list[bytes] = []
        got = 0
        while got < n:
            chunk = self._sock.recv(n - got)
            if not chunk:
                raise EOFError("short read on plasma socket")
            parts.append(chunk)
            got += len(chunk)
        return b"".join(parts)

    def hello(self, client: str = "python") -> dict[str, Any]:
        self._send_frame({"op": "hello", "client": client})
        return self._recv_frame_json()

    def send_sample(
        self,
        *,
        node_id: str,
        cycles: int | None = None,
        instructions: int | None = None,
        ts_ns: int | None = None,
    ) -> dict[str, Any]:
        msg: dict[str, Any] = {"op": "sample", "node_id": node_id}
        if cycles is not None:
            msg["cycles"] = cycles
        if instructions is not None:
            msg["instructions"] = instructions
        if ts_ns is not None:
            msg["ts_ns"] = ts_ns
        else:
            msg["ts_ns"] = time.time_ns()
        self._send_frame(msg)
        return self._recv_frame_json()

    def branch(self, nodes: list[dict[str, Any]], parent: str | None = None) -> dict[str, Any]:
        msg: dict[str, Any] = {"op": "branch", "nodes": nodes}
        if parent is not None:
            msg["parent"] = parent
        self._send_frame(msg)
        return self._recv_frame_json()

    def request_fd(self, kind: str) -> tuple[dict[str, Any], int]:
        self._send_frame({"op": "request_fd", "kind": kind})
        return recv_json_with_fds(self._sock)


def recv_json_with_fds(sock: socket.socket) -> tuple[dict[str, Any], int]:
    """Read one framed JSON message and return (obj, first_scm_rights_fd)."""
    bufsize = _MAX_FRAME + 4
    space = socket.CMSG_SPACE(struct.calcsize("i"))
    data, ancdata, _flags, _addr = sock.recvmsg(bufsize, space)
    if len(data) < 4:
        raise ValueError("short plasma response")
    ln = struct.unpack_from(">I", data, 0)[0]
    end = 4 + int(ln)
    if end > len(data):
        raise ValueError("truncated plasma frame (increase bufsize or check stream)")
    body = data[4:end]
    obj = json.loads(body.decode("utf-8"))

    fds: list[int] = []
    for cmsg_level, cmsg_type, cmsg_data in ancdata:
        if cmsg_level == _SOL_SOCKET and cmsg_type == _SCM_RIGHTS:
            step = struct.calcsize("i")
            for i in range(0, len(cmsg_data), step):
                fds.append(struct.unpack_from("i", cmsg_data, i)[0])

    if obj.get("op") == "error":
        for f in fds:
            try:
                os.close(f)
            except OSError:
                pass
        raise RuntimeError(obj.get("error", "plasma error"))

    if not fds:
        raise RuntimeError("expected SCM_RIGHTS fd in response")

    extra = fds[1:]
    for f in extra:
        try:
            os.close(f)
        except OSError:
            pass
    return obj, fds[0]


def mmap_shm_fd(fd: int, size: int) -> mmap.mmap:
    return mmap.mmap(fd, size, mmap.MAP_SHARED, mmap.PROT_READ | mmap.PROT_WRITE)


def perf_window_count(fd: int, meta: dict[str, Any], *, sleep_ms: int = 40) -> int:
    """Drive a perf_event fd using ioctl codes returned in fd_reply."""
    import fcntl

    reset = int(meta["perf_ioc_reset"])
    enable = int(meta["perf_ioc_enable"])
    disable = int(meta["perf_ioc_disable"])
    fcntl.ioctl(fd, reset, 0)
    fcntl.ioctl(fd, enable, 0)
    time.sleep(sleep_ms / 1000.0)
    fcntl.ioctl(fd, disable, 0)
    buf = os.read(fd, 8)
    if len(buf) != 8:
        raise OSError(f"perf read expected 8 bytes, got {len(buf)}")
    return struct.unpack("Q", buf)[0]


__all__ = [
    "PlasmaClient",
    "mmap_shm_fd",
    "perf_window_count",
    "recv_json_with_fds",
]
