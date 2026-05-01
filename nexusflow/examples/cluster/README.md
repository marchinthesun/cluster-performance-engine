# Cluster quick start

Deploy NexusFlow on **login / compute** Linux nodes (with or without Slurm).

| Path | Purpose |
|------|---------|
| [`install.sh`](../../install.sh) | `install -Dm755` → `$PREFIX/bin/nexusflow` **and** `$PREFIX/bin/kubermetrics` (same binary) |
| [`systemd/nexusflow-dashboard.service.example`](../../systemd/nexusflow-dashboard.service.example) | dashboard unit (TLS / ACL) |
| [`examples/slurm/`](../slurm/) | prolog + sample job |
| [`examples/ci/`](../ci/) | CI-oriented DAG YAML |
| [`examples/ml/`](../ml/) | NUMA-oriented DAG YAML |

Typical flow:

1. Build on a builder host: `go build -o nexusflow ./cmd/nexusflow`.
2. Distribute (Ansible, modules, RPM, `pdsh`/`scp`, …) + [`install.sh`](../../install.sh).
3. In Slurm batch scripts: `eval "$(nexusflow topology hints --format shell)"`, then consume `$NEXUSFLOW_SRUN_EXTRA` / MPI env vars.

See root [`README.md`](../../README.md) for dashboard variables and remote security.
