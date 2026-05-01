#!/usr/bin/env bash
#SBATCH --job-name=nexusflow-demo
#SBATCH --nodes=1
#SBATCH --ntasks=1
#SBATCH --cpus-per-task=4
#
# Example: load pinning hints then launch your MPI/binary inside srun with extra bind flags.

set -euo pipefail
module load nexusflow 2>/dev/null || true

eval "$(nexusflow topology hints --format shell --source auto)"

echo "NexusFlow suggests cpus/task=${NEXUSFLOW_SUGGEST_CPUS_PER_TASK:-?}"
echo "Example srun extras: ${NEXUSFLOW_SRUN_EXTRA:-}"

echo "Replace this block with your workload, e.g.:"
echo "  nexusflow run --cpus \"\${SLURM_CPUS_PER_TASK:-4}\" --numa 0 -- ./your-app \"\$@\""
