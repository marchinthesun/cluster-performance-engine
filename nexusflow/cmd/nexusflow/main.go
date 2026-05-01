package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kube-metrics/nexusflow/pkg/affinity"
	"github.com/kube-metrics/nexusflow/pkg/dashboard"
	"github.com/kube-metrics/nexusflow/pkg/kubermetrics"
	"github.com/kube-metrics/nexusflow/pkg/topology"
)

func usage() {
	fmt.Fprintf(os.Stderr, `NexusFlow — default: kube deploy (if kubeconfig present) + Plasma + dashboard.
  Or invoke as "kubermetrics" / with kube-metrics-only flags. Optional CLI for tooling and dashboard /api/exec.

kubermetrics flags (first argv must start with "-"):  --dry-run  --print-id  --cpu-id …  --kubeconfig …

CLI (for Control deck / automation):
  topology … | run … | dag run … | daemon … | hugepages set … | shm … | perf … | dashboard … | plasma … | version

`)
}

func looksLikeKubermetricsArgv(args []string) bool {
	if len(args) == 0 {
		return false
	}
	a := args[0]
	if a == "-h" || a == "--help" {
		return false
	}
	if !strings.HasPrefix(a, "-") {
		return false
	}
	if strings.HasPrefix(a, "-cpu-id") || strings.HasPrefix(a, "-kubeconfig") {
		return true
	}
	switch a {
	case "-dry-run", "--dry-run", "-print-id", "--print-id", "-cpu-id", "--cpu-id":
		return true
	default:
		return false
	}
}

func main() {
	base := filepath.Base(os.Args[0])
	if strings.EqualFold(strings.TrimSuffix(base, ".exe"), "kubermetrics") {
		kubermetrics.Main(os.Args[1:])
		return
	}
	if len(os.Args) > 1 && looksLikeKubermetricsArgv(os.Args[1:]) {
		kubermetrics.Main(os.Args[1:])
		return
	}
	if len(os.Args) == 1 {
		runSupervisor()
		return
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "topology", "topo":
		topologyCmd(os.Args[2:])
	case "run":
		runCmd(os.Args[2:])
	case "dag":
		if len(os.Args) < 4 || os.Args[2] != "run" {
			fmt.Fprintf(os.Stderr, "usage: nexusflow dag run --file pipeline.yaml [--prom-file PATH]\n")
			os.Exit(2)
		}
		dagRunCmd(os.Args[3:])
	case "shm":
		if len(os.Args) < 4 || os.Args[2] != "create" {
			fmt.Fprintf(os.Stderr, "usage: nexusflow shm create --size BYTES\n")
			os.Exit(2)
		}
		shmCreateCmd(os.Args[3:])
	case "perf":
		if len(os.Args) < 4 || os.Args[2] != "sample" {
			fmt.Fprintf(os.Stderr, "usage: nexusflow perf sample [--sleep-ms N] [--kind cycles|instructions]\n")
			os.Exit(2)
		}
		perfSampleCmd(os.Args[3:])
	case "dashboard":
		dashboardCmd(os.Args[2:])
	case "daemon":
		daemonMain(os.Args[2:])
	case "hugepages":
		if len(os.Args) < 4 || os.Args[2] != "set" {
			fmt.Fprintf(os.Stderr, "usage: nexusflow hugepages set --pages N [--size 2M|1G]  (same with --count)\n")
			os.Exit(2)
		}
		hugepagesSetCmd(os.Args[3:])
	case "-h", "--help", "help":
		usage()
	case "version":
		fmt.Println(dashboard.BinaryVersion())
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func topologyCmd(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "hints":
			topologyHintsCmd(args[1:])
			return
		case "matrix":
			topologyMatrixCmd(args[1:])
			return
		}
	}

	fs := flag.NewFlagSet("topology", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "emit topology as JSON")
	source := fs.String("source", "auto", "topology source: sysfs, hwloc, auto")
	_ = fs.Parse(args)

	t, srcUsed, err := discoverTopology(*source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology: %v\n", err)
		os.Exit(1)
	}
	if *jsonOut {
		b, err := topology.MarshalTopologyJSONIndent(t, "", "  ", srcUsed)
		if err != nil {
			fmt.Fprintf(os.Stderr, "json: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
		return
	}
	fmt.Print(topology.FormatASCII(t))
}

func runCmd(args []string) {
	split := -1
	for i, a := range args {
		if a == "--" {
			split = i
			break
		}
	}
	if split < 0 || split == len(args)-1 {
		fmt.Fprintf(os.Stderr, "run: missing command after -- (example: nexusflow run --cpus 4 -- ./app)\n")
		os.Exit(2)
	}
	flagPart := args[:split]
	cmdPart := args[split+1:]

	fs := flag.NewFlagSet("run", flag.ExitOnError)
	cpus := fs.Int("cpus", 0, "number of logical CPUs to use (0 = all)")
	numaFlag := fs.Int("numa", -1, "prefer this NUMA node id (default: auto pick largest domain)")
	strategy := fs.String("strategy", "same-numa", "placement strategy (same-numa)")
	priority := fs.String("priority", "normal", "nice before exec: high|normal|low (high often needs root/CAP_SYS_NICE)")
	membind := fs.Bool("membind", true, "with numactl on PATH: bind memory to the NUMA node of the CPU set")
	if err := fs.Parse(flagPart); err != nil {
		os.Exit(2)
	}

	t, err := topology.Discover()
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology: %v\n", err)
		os.Exit(1)
	}

	var numaPrefer *int
	if *numaFlag >= 0 {
		numaPrefer = numaFlag
	}

	cpuList, err := topology.SelectCPUs(t, *cpus, numaPrefer, topology.SelectionStrategy(*strategy))
	if err != nil {
		fmt.Fprintf(os.Stderr, "select cpus: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "nexusflow: pinning to %d CPUs: %v\n", len(cpuList), cpuList)

	var opts affinity.RunOpts
	switch strings.ToLower(strings.TrimSpace(*priority)) {
	case "normal", "":
	case "high":
		n := -10
		opts.Nice = &n
	case "low":
		n := 10
		opts.Nice = &n
	default:
		fmt.Fprintf(os.Stderr, "run: unknown --priority %q (use high|normal|low)\n", *priority)
		os.Exit(2)
	}

	argv := cmdPart
	if *membind {
		if node, ok := topology.PrimaryNUMANodeForCPUs(t, cpuList); ok {
			if nu, err := exec.LookPath("numactl"); err == nil {
				wrapped := make([]string, 0, len(cmdPart)+6)
				wrapped = append(wrapped, nu,
					fmt.Sprintf("--cpunodebind=%d", node),
					fmt.Sprintf("--membind=%d", node),
					"--")
				wrapped = append(wrapped, cmdPart...)
				argv = wrapped
			}
		}
	}

	if err := affinity.Run(cpuList, argv, opts); err != nil {
		fmt.Fprintf(os.Stderr, "run: %v\n", err)
		os.Exit(1)
	}
}
