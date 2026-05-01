# NexusFlow — Security

NexusFlow interacts with **Linux kernel primitives** (`sched_setaffinity`, `perf_event_open`, `mmap`, Unix **SCM_RIGHTS**) and may expose an **HTTP dashboard** that can spawn **allow-listed subprocesses**. This document is the trust anchor for security architects, platform engineers, and compliance reviewers.

---

## 1. Threat model

### 1.1 Assets

| Asset | Description |
|-------|-------------|
| **Host CPU / memory** | Correctness and performance of co-located workloads |
| **Secrets in env / files** | Inherit to child processes spawned via `run`, `dag`, `plasma`, dashboard `/api/exec` |
| **POSIX shm segments** | Fast IPC blobs under `/dev/shm` |
| **Unix sockets** | Plasma control; file mode defines who can connect on-disk |
| **Telemetry** | Prometheus text files; perf counter metadata |

### 1.2 Adversaries

| Actor | Objective |
|-------|-----------|
| **Local unprivileged user** | Read/write peers’ shm if mis-permissioned; connect to loose Unix sockets; abuse dashboard |
| **Workload in same UID** | Map same `/dev/shm` name; inherit fds if passed recklessly |
| **Network attacker** | Reach dashboard, **gRPC daemon**, or misbound services |

### 1.3 Memory isolation (shared memory)

**Mechanism:**

- `pkg/shm` creates files with **`0600`** (owner read/write only).
- Random suffixes or **exclusive** named paths reduce accidental collisions.
- **No encryption at rest** inside shm; treat contents as **trusted process memory only**.

**Guarantee:** Process **A** (UID **X**) cannot mmap NexusFlow shm of process **B** (UID **Y**) unless the OS file permissions are weakened by the operator (e.g. `chmod`, shared user namespace misuse).

**Non-goals:** NexusFlow does **not** implement **cryptographic** segmentation between peers on the **same UID**. Use **separate users**, **containers**, or **VMs** for tenant isolation.

### 1.4 Side-channel and shared-CPU risk

**NUMA pinning** reduces migration noise but **does not**:

- Flush micro-architectural buffers between **untrusted** peers on **sibling hyperthreads**
- Replace **confidential computing** (SEV, TDX) or **scheduler policies** for classified mixed workloads

**Guidance:** For **adversarial multi-tenant** CPUs, combine **physical core reservation**, **nosmt**, **cgroups**, and organizational **segmentation**; NexusFlow is a **performance** tool first.

### 1.5 Supply chain

- Build from **tagged** releases and verify **checksums**
- Pin **container base images** and rebuild on CVE
- Python SDK: **`pip install -e`** from a vetted path; **`NEXUSFLOW_BIN`** must point to a **known** `nexusflow` binary

---

## 2. Privilege requirements

### 2.1 Capability and sysctl matrix

| Feature | Typical requirement |
|---------|---------------------|
| **`nexusflow run` (`sched_setaffinity` + optional `setpriority`)** | CPU mask: typically allowed for same user; may be **restricted by cgroups** or **systemd** `AllowedCPUs`. `--priority high` sets a **lower nice** (e.g. −10) and often needs **`CAP_SYS_NICE`** or superuser. |
| **`taskset` (DAG)** | Same as above for target PIDs |
| **`perf sample` / `perf_event_open`** | Often needs `kernel.perf_event_paranoid <= 1` (site policy) or **`CAP_PERFMON`** / **`CAP_SYS_ADMIN`** depending on kernel |
| **`/dev/shm` create** | Writable `/dev/shm` for the **effective UID** |
| **Plasma Unix listen** | Create socket in path owned / writable by service user; avoid **world-writable** run dirs on shared login nodes |
| **Dashboard HTTP(S)** | **Bind** to port <1024 may need **`CAP_NET_BIND_SERVICE`**; prefer ≥1024 or reverse proxy |
| **`nexusflow daemon` (gRPC)** | **TCP listener**: anyone who can connect can call **`CreateCell`**, **`AttachPID`**, **`RunInCell`**, **`WatchL3`**, **`SetHugepages`**, **`EvictForeign`** as the daemon’s **UID**. Treat like **root-equivalent** on the box unless fronted by **mTLS**, **Unix forward**, or **localhost + strict firewall**. |
| **cgroup v2 cells (`pkg/cgv2`)** | Writing **`cpuset.cpus`**, **`cpuset.mems`**, **`cgroup.procs`** under **`/sys/fs/cgroup`** typically requires **superuser** or **delegated** cgroup subtree owned by the service user. Misconfiguration can **disrupt** other workloads on the host. |
| **`EvictForeign` (`pkg/evict`)** | **`kill(2)`** (default **`SIGSTOP`**) on PIDs whose affinity **intersects** the target CPU set. Needs **`CAP_KILL`** for **foreign** processes (or same user), and can cause **loss of service** for other tenants. |
| **`SetHugepages` / `hugepages set`** | Writes **`/sys/kernel/mm/hugepages/.../nr_hugepages`**: normally **root** or **`CAP_SYS_ADMIN`**. Can affect **global** memory pressure and boot-time policy. |
| **LLC perf (`WatchL3`)** | Same constraints as **`perf_event_open`** (see **`perf sample`** row); samples on a **lead CPU** for the cell. |

### 2.2 Least-privilege deployment checklist

- [ ] Run as **dedicated** service user (non-root) with **no** shell login
- [ ] Dashboard: **`127.0.0.1`** OR **TLS + `Authorization: Bearer` + `--allow-cidr`**
- [ ] **systemd**: `NoNewPrivileges=yes`, `PrivateTmp=yes`, `ProtectSystem=strict`, drop **capabilities** not needed
- [ ] Containers: **read-only root** where possible; explicit **`/dev/shm` size** limit
- [ ] **Never** expose Plasma socket to **global-writable** directories on multi-tenant hosts
- [ ] Set **`timeout_sec`** on dashboard exec to bound runaway commands
- [ ] **Daemon (gRPC)**: bind **`127.0.0.1`**, or front with **mTLS** / **SSH tunnel** / **Unix** forward; treat RPC surface like **host admin** access
- [ ] **Eviction / hugepages**: grant **`CAP_KILL`** / **sysctl/sysfs** rights only to a **narrow** automation identity; document **SIGCONT** for stopped foreign workloads

---

## 3. Dashboard-specific risks

`POST /api/exec` executes **argv arrays** drawn from an **allow-list** of command roots (`topology`, `run`, `dag`, `shm`, `perf`, `plasma`, `hugepages`, …).

| Risk | Control |
|------|---------|
| **Remote code execution** | Equivalent to **local CLI** access—treat as **privileged** |
| **Token bypass** | Always set **`--auth-token`** for non-loopback |
| **SSRF / relay** | Restrict **`--allow-cidr`** to admin networks |
| **DoS** | Subprocess wall-clock **timeout**; HTTP **IdleTimeout** |

---

## 4. Audit trail and compliance posture

| Area | NexusFlow behavior | Enterprise mapping |
|------|-------------------|-------------------|
| **Who changed affinity?** | OS auditd can trace `execve` of `nexusflow`; dashboard can be fronted by auth proxy with **request logs** |
| **Pipeline timings** | DAG Prometheus metrics → SIEM / TSDB |
| **Plasma branch / fd** | Coordinator **logs** significant events; centralize via **journald** / **sidecar** |
| **SOC2 / ISO** | NexusFlow does **not** auto-produce control evidence; **you** map controls to **logging**, **access**, **change** tickets |

---

## 5. Vulnerability disclosure

We take security reports seriously.

### 5.1 How to report

**Preferred:** GitHub **Private vulnerability reporting** (enable in repo **Settings → Security**).

**Alternative:** Email the project maintainers at the address listed in the repository **MAINTAINERS** file or organization contact page. If none is published, open a **non-public** support channel your organization uses for OSS intake.

Include:

- Affected **component** (CLI, dashboard, Plasma, shm, kubermetrics, **daemon gRPC**, **cgroup cells**, **eviction**, **hugepages**, etc.)
- **Reproduction** steps on a **stock** Linux distro
- **Impact** (confidentiality / integrity / availability)
- Optional: **patch** idea or diff

### 5.2 PGP (optional)

If your policy requires encrypted mail, publish a **`security.asc`** key in-repo and reference the **fingerprint** here. *Placeholder: add `security@yourdomain.com` + key ID when available.*

### 5.3 Response expectations

| Phase | Target |
|-------|--------|
| **Acknowledgement** | ≤ 5 business days |
| **Triage** | Severity + affected versions |
| **Fix** | Patched release or documented mitigation |
| **Credit** | CVE / advisory + reporter credit if desired |

---

## 6. Known limitations (security-relevant)

- **Shared UID** shm peers can read each other’s segments if they guess names—use **random** shm from CLI or unguessable secrets in filenames under your policy.
- **kubermetrics** manifests deploy workloads with strong defaults (**non-root**, **readOnlyRootFilesystem**, **NetworkPolicy**); review before **forking** for your tenancy model.
- **hwloc / sysfs** parsers assume **non-malicious** kernel output; **do not** run untrusted **fake sysfs** in the same trust domain as production secrets.
- **Daemon NUMA memory policy** is implemented via **cgroup v2 `cpuset.mems`** (and CPU masks), not a raw **`mbind(2)`** syscall in-process; callers who need explicit **`mbind`** must combine NexusFlow with **their own** memory policy or future API extensions.
- **Eviction** is **best-effort**: races, **immutable** affinities, and **namespaced** PIDs can limit effectiveness; **SIGSTOP** can leave processes **frozen** until **`SIGCONT`**—document **ops** procedures before enabling automation.

---

## 7. Related reading

- `ARCHITECTURE.md` — data plane and placement
- `README.md` — benchmarks and entry points
- `nexusflow/README.md` — operational hardening (dashboard + CLI surfaces)
