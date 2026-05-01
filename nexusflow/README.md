# NexusFlow

Go/Python tooling for **NUMA-aware** placement on Linux: topology (**sysfs** or **hwloc XML**), pinning (`sched_setaffinity` / `taskset`), lightweight **DAG** pipelines, **POSIX shared memory** under `/dev/shm`, **perf** counters (fds suitable for **SCM_RIGHTS**).

Not a Kubernetes replacement; goal is low per-node overhead and predictable CPU/memory affinity.

## Build

```bash
cd nexusflow
go build -o nexusflow ./cmd/nexusflow
```

Module participates in repo-root `go.work`.

## CLI

```bash
# Topology: sysfs (default), hwloc (when hwloc-ls/lstopo installed), auto = sysfs then hwloc fallback
nexusflow topology --source auto
nexusflow topology --json --source sysfs
nexusflow topology hints --format shell --source auto   # export NEXUSFLOW_* for Slurm/MPI wrappers
nexusflow topology hints --format json
nexusflow topology hints --format openmpi               # MCA / mpirun hints
nexusflow topology hints --format slurm                 # shell exports + Slurm preamble
nexusflow topology matrix --source auto                 # make -j / NUMA matrix text for CI logs

# Pin: CPU + optional same-NUMA memory (numactl) + optional nice
nexusflow run --cpus 8 --numa 0 --priority normal --membind=true -- make -j8
nexusflow run --cpus 16 --priority high -- ./heavy_sim

# DAG from YAML (taskset when cpus>0); Prometheus text metrics:
nexusflow dag run --file examples/pipeline.yaml --prom-file /tmp/nf-dag.prom

# Shared memory (prints NEXUSFLOW_SHM_* for peers; segment stays under /dev/shm)
nexusflow shm create --size 1048576
nexusflow shm create --size 65536 --name myseg

# Plasma: Unix DAG coordinator + dynamic branch(...) over socket, shm, SCM_RIGHTS fds (Linux)
nexusflow plasma run --file examples/plasma/plasma.yaml --listen /run/plasma.sock --shm-name dockdemo

# Perf: hardware cycles/instructions window; fd can be dup’d/sent over Unix socket
nexusflow perf sample --sleep-ms 100 --kind cycles

# Unified daemon (Linux): cgroup v2 cells, L3 cache counters stream, eviction RPC, hugepages RPC
# Serve gRPC on a loopback or VPN-only address; mTLS/reverse proxy recommended off single-user nodes.
nexusflow daemon --listen 127.0.0.1:50051
# Optional: parent cgroup path (default under /sys/fs/cgroup via NEXUSFLOW_CGROUP_ROOT in code paths)
nexusflow daemon --listen 127.0.0.1:50051 --cgroup-root /sys/fs/cgroup/nexusflow-daemon

# HugePages from CLI (writes nr_hugepages sysfs; typically root)
nexusflow hugepages set --size 2M --pages 128
# `--count` is an alias for `--pages` when you prefer that name
nexusflow hugepages set --size 1G --count 2

# Dashboard (TLS + ACL + Bearer recommended off localhost)
nexusflow dashboard --listen 127.0.0.1:9842
nexusflow dashboard --listen 0.0.0.0:9842 \
  --tls-cert /etc/nexusflow/cert.pem --tls-key /etc/nexusflow/key.pem \
  --auth-token "$NF_DASHBOARD_TOKEN" \
  --allow-cidr 10.0.0.0/8 --allow-cidr 127.0.0.1/32
```

The **`--`** separator is required before the wrapped command in **`run`**.

### Dashboard

**`nexusflow dashboard`** serves a compact **control deck** (static UI) and **`POST /api/exec`**, spawning the **same binary** with argv (no shell).

Main areas: **Output** (command bar, transcript, stdout/stderr, run log, **Tables & charts** with NUMA bars + TSV parsing) · **Topology** (per-CPU grid from last JSON) · **Control** (whitelist argv builders, including **hugepages set**) · **Guide** (product summary + **`help`** shortcut).

Allowed roots: **`topology`** (including `hints`, `matrix`), **`topo`**, **`run`**, **`dag`**, **`shm`**, **`perf`**, **`plasma`**, **`hugepages`**, **`help`** (`-h` / `--help`). Strings like `topology hints --format shell` pass validation because argv[0] is `topology`.

Hardening when exposed:

- **`--tls-cert` / `--tls-key`** — HTTPS.
- **`--auth-token`** — `POST /api/exec` requires `Authorization: Bearer <token>` (sidebar field in UI).
- **`--allow-cidr`** (repeatable) — reject peers outside listed CIDRs (`RemoteAddr`).

Example unit: `systemd/nexusflow-dashboard.service.example`.

### Cluster rollout

Summary: `examples/cluster/README.md`.

1. Build static or dynamically linked binary (`go build`).
2. Push to login/compute nodes (`install.sh`, Ansible, modules, RPM, …).
3. In Slurm jobs / modules: `eval "$(nexusflow topology hints --format shell)"`.
4. Optional: **`dag run --prom-file`** writes Prometheus text for **node_exporter** textfile collector.

### Observability (DAG → Prometheus)

**`--prom-file`** on **`nexusflow dag run`** emits **`nexusflow_dag_step_duration_seconds`** and **`nexusflow_dag_step_exit_code`** with `pipeline`, `step` labels. OTLP export can wrap the same timings later; text exposition fits common on-node scraping.

Open MPI MCA names vary by version; prefer **`NEXUSFLOW_MPIRUN_HINT`** or **`ompi_info`** on the target system.

### Plasma (recursive task graph)

**`nexusflow plasma run`** listens on a Unix socket, creates **`/dev/shm/nexusflow-{name}`**, runs an initial YAML DAG, and accepts **`branch`**, **`sample`**, **`request_fd`** (`shm`, `perf_*`) with **`fd_reply`** + **SCM_RIGHTS**. Python: **`nexusflow_sdk.plasma`** (`PlasmaClient`, `mmap_shm_fd`, `perf_window_count`).

From repo root:

```bash
docker compose -f docker-compose.plasma.yml up --build
```

Dashboard: **http://127.0.0.1:9842/** (port mapped in `docker-compose.plasma.yml`; coordinator socket **`/run/plasma.sock`** inside the container).

```bash
docker compose -f docker-compose.plasma.yml exec -it plasma-demo bash
```

## Go packages

| Package | Role |
|---------|------|
| `pkg/topology` | `Discover()`, JSON marshal/unmarshal, `SelectCPUs`, ASCII, **`BuildClusterHints`** / Slurm-MPI exports (`WriteHintsShell`, …) |
| `pkg/hwloc` | `DiscoverCLI()`, `TopologyFromXML` |
| `pkg/affinity` | `Run` (exec), `Command` (taskset for DAG) |
| `pkg/dag` | YAML pipeline, `TopoOrder`, `Runner` |
| `pkg/shm` | POSIX `/dev/shm` + shared mmap (`Create`, `CreateNamed`, `OpenPath`) |
| `pkg/perf` | `PerfEventOpen`, counter read, `FD()` |
| `pkg/plasma` | Unix coordinator + dynamic branches, samples, fds |
| `pkg/cgv2` | cgroup v2 **cell** manager: `cpuset.cpus` / `cpuset.mems`, attach PIDs |
| `pkg/daemon` | gRPC server (`Serve`); **`nexusflow daemon`** (Linux) |
| `pkg/evict` | Scan `/proc`, affinity vs CPU set, optional **`kill`** (eviction RPC) |
| `pkg/hugepages` | **`nr_hugepages`** sysfs for 2M / 1G |
| `api/v1` | Generated stubs from **`nexusflow.proto`** (`NexusFlowDaemon` service) |
| `pkg/nexusflow` | Combined SDK (`DiscoverTopology`, `TopologyJSON`, `LoadDAGYAML`, `RunDAG`, …) |

### SDK snippet

```go
import nf "github.com/kube-metrics/nexusflow/pkg/nexusflow"

t, err := nf.DiscoverTopology()
j, err := nf.TopologyJSON(t, "sysfs")
```

## Python SDK

Directory `sdk/python/`, package **`nexusflow_sdk`**:

- `discover_sysfs_linux()` — sysfs-only (no Go binary).
- `discover_go_cli()` / `discover(prefer_cli=True)` — runs `nexusflow topology --json`.
- `PlasmaClient` — Unix socket + SCM_RIGHTS (`request_fd`), samples under `examples/plasma/*.py`.
- JSON matches Go `Snapshot` (`cpus`, `numa_nodes`, `source`).

```bash
pip install -e sdk/python
python -c "from nexusflow_sdk import discover; print(discover().dumps_indent())"
```

**`NEXUSFLOW_BIN`** selects the Go binary path for CLI-backed discovery.

## DAG YAML

See `examples/pipeline.yaml`: `id`, `cmd`, `cpus`, `numa_node`, `depends_on`.

More samples:

- `examples/ci/build-ci-sample.yaml` — CI-style DAG referencing `NEXUSFLOW_MAKE_J_LOGICAL`.
- `examples/ml/etl-numa-dag.yaml` — NUMA-separated stages.

## Production readiness

- **Dashboard probes**: `GET /healthz` and `GET /health` return JSON `{status,service,version,time_utc}` without authentication (safe behind network policy). Logs skip noise from these paths.
- **Release identity**: run `nexusflow version` (module semver when tagged, otherwise `devel-<gitsha>` when built from VCS).
- **Remote dashboard**: always combine **TLS**, **`--auth-token`**, and **`--allow-cidr`** if `Listen` is not loopback-only; `/api/exec` is equivalent to arbitrary allow-listed CLI execution.
- **Timeouts**: subprocess wall clock for `/api/exec` is capped (`timeout_sec`, max 7200); HTTP server sets **IdleTimeout** (120s); long-running Plasma/DAG jobs should raise `timeout_sec` from the UI **Control** tab or tune reverse-proxy timeouts.
- **Coordinator shm**: Plasma removes stale `/dev/shm/nexusflow-<name>` before `O_EXCL` create — **never reuse the same `--shm-name` across two live coordinators**.
- **DAG metrics**: `--prom-file` overwrites the target path (`os.WriteFile`); align ownership/mode with node_exporter **textfile collector** expectations.

## Limitations

- Full functionality targets **Linux**.
- **`nexusflow daemon`**, **`pkg/cgv2`**, **`pkg/evict`**, **`pkg/hugepages`** (set), and **`WatchL3`** are **Linux-only**; **`nexusflow daemon`** fails fast on other OSes.
- **`nexusflow run`**: optional **NUMA memory** bind uses **`numactl`** on `PATH`; without it, only CPU affinity is enforced.
- DAG pins children via **`taskset`** (util-linux).
- **perf** needs rights for `perf_event_open` (similar to `perf stat`).
- **hwloc** optional; without tools use `--source sysfs`.

## Next steps

Kubernetes device plugins, Rust core, richer scheduling graphs — out of scope here; packages are split for extension.
