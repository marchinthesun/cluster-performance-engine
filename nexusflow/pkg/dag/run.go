package dag

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/kube-metrics/nexusflow/pkg/affinity"
	"github.com/kube-metrics/nexusflow/pkg/topology"
)

// Runner executes pipeline nodes sequentially in topological order.
type Runner struct {
	Topo *topology.Topology
	// PromTextFile, when set, receives Prometheus exposition text after full success or immediately after the first failing node (partial series).
	PromTextFile string
}

// Run executes each node under optional CPU pinning (Linux: util-linux taskset).
func (r *Runner) Run(ctx context.Context, p *Pipeline) error {
	if r.Topo == nil {
		return fmt.Errorf("runner: topology required")
	}
	order, err := TopoOrder(p.Nodes)
	if err != nil {
		return err
	}

	var timings []StepTiming

	for _, n := range order {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		cpus, err := topology.SelectCPUs(r.Topo, n.CPUs, n.NUMANode, topology.StrategySameNUMA)
		if err != nil {
			return fmt.Errorf("node %q: select cpus: %w", n.ID, err)
		}

		baseCmd, err := affinity.Command(cpus, n.Cmd)
		if err != nil {
			return fmt.Errorf("node %q: %w", n.ID, err)
		}

		cmd := exec.CommandContext(ctx, baseCmd.Args[0], baseCmd.Args[1:]...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Env = os.Environ()

		fmt.Fprintf(os.Stderr, "nexusflow dag: running node %q cpus=%v cmd=%v\n", n.ID, cpus, n.Cmd)
		t0 := time.Now()
		runErr := cmd.Run()
		dur := time.Since(t0)
		exit := 0
		if runErr != nil {
			var ee *exec.ExitError
			if errors.As(runErr, &ee) {
				exit = ee.ExitCode()
			} else {
				exit = -1
			}
			timings = append(timings, StepTiming{ID: n.ID, Duration: dur, ExitCode: exit})
			_ = WritePrometheusTextFile(r.PromTextFile, p.Name, timings)
			return fmt.Errorf("node %q: %w", n.ID, runErr)
		}
		timings = append(timings, StepTiming{ID: n.ID, Duration: dur, ExitCode: 0})
	}

	if err := WritePrometheusTextFile(r.PromTextFile, p.Name, timings); err != nil {
		return fmt.Errorf("prometheus text file: %w", err)
	}
	return nil
}
