# NexusFlow — Architecture

This document is the systems-level map for operators, SREs, and HPC architects. It explains how NexusFlow observes hardware, chooses CPU sets, runs work, and moves data with minimal kernel/user churn.

**Implementation today:** Go 1.22 (`github.com/kube-metrics/nexusflow`). Layout is modular so a future **high-frequency scheduler core** (e.g. Rust) or a **Kubernetes device plugin** can replace or augment pieces without rewriting topology clients.

---

## 1. Design goals

| Goal | Mechanism |
|------|------------|
| **Predictable tail latency** | NUMA-local CPU sets, fewer cross-socket hops |
| **Low orchestration overhead** | Direct `sched_setaffinity` + `exec`, thin coordinator processes |
| **Observable pipelines** | DAG → Prometheus text; optional `perf` fds |
| **Fast data plane between co-located peers** | POSIX shared memory + Unix sockets + SCM_RIGHTS |
| **No Kubernetes replacement** | Compose with Slurm, systemd, bare CI agents, or sidecars |

---

## 2. Hardware topology mapping

### 2.1 Sources

NexusFlow builds a **snapshot** of the machine into `topology.Topology`:

| Source | Package / entry | When to use |
|--------|------------------|-------------|
| **sysfs** | `pkg/topology` → `Discover()` on Linux | Default; no extra packages |
| **hwloc** | `pkg/hwloc` — CLI XML or file | Richer caches / objects when `hwloc-ls` / `lstopo` is installed |

The snapshot includes:

- **Logical CPUs** — id, package (socket), core, optional **thread siblings**
- **NUMA nodes** — id, list of CPUs, optional **distance** vector when exported by firmware/OS

This is the **hardware graph** feeding every downstream decision.

### 2.2 Graph shape

Conceptually:

```text
Socket (Package) ──┬── NUMA node 0 ── CPUs {0..n}
                   ├── NUMA node 1 ── CPUs {…}
                   └── (distance matrix may link nodes)
```

Operators consume the same graph as:

- **JSON** — for tools and tests
- **Cluster hints** — `BuildClusterHints`: Slurm/MPI/OpenMPI/shell exports (`pkg/topology/hints.go`)
- **ASCII matrix** — CI-friendly `topology matrix` for “what `make -j` should I use?”

---

## 3. Scheduling and placement logic

NexusFlow is **not** a global batch scheduler; it is a **local** placement engine on each node (or within each job step).

### 3.1 Strategy: `same-numa`

Implemented in `pkg/topology/placement.go` (`SelectCPUs`):

1. **Explicit NUMA node** (`--numa N`): if the node has at least `want` CPUs, return the lowest logical ids on that node (sorted).
2. **Auto**: among NUMA nodes with **≥ want** CPUs, pick the node with the **largest** CPU count (ties broken by **lower node id**).
3. **Spill**: if no single node fits, walk nodes in order and accumulate CPUs until `want` is satisfied.

This encodes a practical priority:

**NUMA locality (fit on one domain) → largest domain (more headroom / often better bandwidth) → deterministic tie-break.**

### 3.2 Affinity scoring (conceptual mapping)

When comparing to research schedulers that score “L3 → NUMA → socket distance”:

| Research signal | NexusFlow analogue |
|-----------------|-------------------|
| **L3 / cache locality** | Not modeled per cache today; **same-numa** proxy-couples cores that share closer memory |
| **NUMA proximity** | Primary: stay on **one** node when possible |
| **Socket distance** | Indirect: sysfs/hwloc **distance** vectors are available on `NUMANode` for future scoring |

Extensions (out of tree) can replace `SelectCPUs` with weighted scoring without changing the CLI surface.

### 3.3 Context switch reduction

NexusFlow reduces **involuntary migration** by:

| Mechanism | Effect |
|-----------|--------|
| **`nexusflow run`** | `sched_setaffinity` on the **thread** that calls `execve`, locking the **replacement** workload to a cpuset |
| **`dag run`** | Spawns children with **`taskset`** when `cpus` / topology is specified |
| **Topology-aware `make -j`** hints | Fewer concurrent tasks than **logical CPUs** across wrong NUMA domains |

Complete elimination of kernel preemption is **not** claimed: **real-time** policies and **cgroup cpu** limits remain the platform owner’s knobs.

---

## 4. Execution plane

### 4.1 Process replacement (`nexusflow run`)

Implemented in `cmd/nexusflow/main.go` + `pkg/affinity/run_linux.go`.

1. **`SelectCPUs`** — choose a **same-NUMA** set (unless the topology is single-node / flat).
2. **Optional `numactl`** — when `--membind` is true (default) and **all** selected CPUs belong to one NUMA node (`topology.PrimaryNUMANodeForCPUs`), and `numactl` is on `PATH`, the command line is wrapped as  
   `numactl --cpunodebind=N --membind=N -- <argv…>` so the **child’s allocations** default to local DRAM, not only CPU affinity.
3. **`exec.LookPath(argv[0])`** — resolve the binary to run (after wrapping, `argv[0]` may be `numactl`).
4. **`runtime.LockOSThread()`** — pin the goroutine about to exec.
5. **Optional `setpriority`** — `--priority=high|normal|low` maps to a **nice** adjustment before exec (`high` ⇒ **nice value −10**; often needs elevated privileges).
6. **`sched_setaffinity(0, cpu_set)`** — apply the CPU mask to the calling thread (then inherited through `exec`).
7. **`syscall.Exec`** — replace the `nexusflow` process with the target program (or `numactl` stub).

If **`numactl`** is missing, NexusFlow still applies **CPU** affinity and logs a hint; memory policy falls back to kernel default.

### 4.2 DAG runner (`nexusflow dag run`)

`pkg/dag`:

- Parse YAML (`id`, `cmd`, `cpus`, `numa_node`, `depends_on`)
- Topological order
- `pkg/affinity.Command` → **`taskset`**-wrapped subprocesses on Linux

Optional **`--prom-file`** emits:

- `nexusflow_dag_step_duration_seconds`
- `nexusflow_dag_step_exit_code`

…for node_exporter textfile collector or custom scrapers.

---

## 5. Data plane (Plasma + Nexus-Bridge semantics)

### 5.1 Shared memory (`pkg/shm`)

Linux implementation:

- Files under **`/dev/shm`**
- **`O_CREAT | O_EXCL`**, mode **0600** (user-private)
- `ftruncate` → `mmap(..., MAP_SHARED, PROT_READ|PROT_WRITE)`

Named segments use a **slug** pattern `nexusflow-{name}` with validation to avoid path injection.

### 5.2 Plasma coordinator (`pkg/plasma`)

**Control plane:** Unix **stream** socket (configurable path, e.g. `/run/plasma.sock`).

**Data plane:**

- Coordinator creates a **shm** segment for fast peer buffers
- Clients may **`request_fd`** (shm, perf types) and receive **`fd_reply`** with **SCM_RIGHTS** (`golang.org/x/sys/unix.Sendmsg`)

This implements **descriptor handoff** so peers **mmap the same backing file** or **attach perf** without re-opening paths over the network.

### 5.3 Dynamic graph

The coordinator supports **branch** requests: new DAG nodes added at runtime; completion tracking and idle shutdown are internal to `coordinator_linux.go`.

---

## 6. Observation

| Path | Output |
|------|--------|
| **DAG metrics** | Prometheus text file (`--prom-file`) |
| **`nexusflow perf sample`** | `perf_event_open`-backed counters; **FD()** for passing via Plasma |
| **Dashboard** | `/healthz`, `/health` JSON; `POST /api/exec` for allow-listed diagnostics |

---

## 7. Integration layer

### 7.1 Interactive / CI agent

- Direct CLI on bare metal or VM
- **`systemd`** unit for dashboard (see `nexusflow/README.md` example paths)

### 7.2 Slurm / MPI

- `topology hints --format slurm|openmpi|shell` exports consumable env vars / snippets

### 7.3 Kubernetes-adjacent

- **Not** an in-cluster scheduler patch
- **Sidecar pattern:** container with shared `hostPID` / sufficient **capabilities** / `SYSFS` mounts as **your policy allows**, running `nexusflow run` or exporting hints
- **`kubermetrics`** (same repo) applies opinionated manifests for a separate operational use case; treat as **optional** automation, not the core data-plane architecture

### 7.4 Docker / compose

- `docker-compose` examples for Plasma + dashboard in `nexusflow` tree

---

## 8. Failure modes (architecture-level)

| Scenario | Mitigation |
|----------|------------|
| **Wrong topology source** | Force `--source sysfs` if hwloc maps stale XML |
| **OOM on shm** | Size segments explicitly; monitor `/dev/shm` |
| **Stale Plasma shm** | Coordinator unlinks previous `nexusflow-{name}` before `O_EXCL` create—**never** reuse one name across two live coordinators |
| **Dashboard exposed** | TLS + bearer + CIDR; see `SECURITY.md` |

---

## 9. Roadmap (extensions)

- Per-L3 **scoring** beyond **same-numa** heuristics
- Optional **Rust** hot path for tick-sensitive loops
- Kubernetes **device plugin** advertising `nexusflow/cpu-cell`

---

## 10. Unified daemon and gRPC (Linux)

| Component | Package / path | Role |
|-----------|----------------|------|
| API | `api/v1/nexusflow.proto` | `NexusFlowDaemon` service definition |
| cgroup v2 cells | `pkg/cgv2` | Creates `cpuset.cpus`, `cpuset.mems`, optional `isolated` partition under `NEXUSFLOW_CGROUP_ROOT` or `/sys/fs/cgroup/nexusflow-daemon` |
| gRPC server | `pkg/daemon` (`Serve`) | Binds TCP, registers generated service |
| LLC sampling | `pkg/perf/cache_linux.go` | `PERF_TYPE_HW_CACHE` last-level **read access / miss** on cell lead CPU |
| Eviction | `pkg/evict` | `SchedGetaffinity` scan + `kill(2)` |
| Hugepages | `pkg/hugepages` | Writes `nr_hugepages` sysfs nodes |

**CLI:** `nexusflow daemon [--listen ADDR] [--cgroup-root PATH]`

---

## 11. Document map

| File | Audience |
|------|----------|
| `README.md` | Executive + benchmarks + quick start |
| `docs/PRODUCT-VISION.md` | Daemon + product summary |
| `ARCHITECTURE.md` | Platform / HPC architects (this file) |
| `SECURITY.md` | Security reviewers, compliance, SecOps |
| `nexusflow/README.md` | Day-to-day CLI reference |
