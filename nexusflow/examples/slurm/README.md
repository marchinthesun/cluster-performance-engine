# Slurm + NexusFlow

## Env exports (CUDA_VISIBLE_DEVICES analogue for CPU locality)

After `eval "$(nexusflow topology hints --format shell --source auto)"` you get `NEXUSFLOW_*`, including:

- `NEXUSFLOW_SUGGEST_CPUS_PER_TASK` — hint for `--cpus-per-task`.
- `NEXUSFLOW_SRUN_EXTRA` — example bind flags for `srun`.
- `NEXUSFLOW_MPIRUN_HINT` — suggested args for Open MPI `mpirun`.

Slurm sets `SLURM_*`; NexusFlow adds **node-local geometry** before launching user apps.

## JobProlog / profile snippet

Wire [`prolog-hints.sh`](prolog-hints.sh) only with site approval (do not replace system prolog blindly).

## Batch example

See [`job-affinity.sh`](job-affinity.sh).
