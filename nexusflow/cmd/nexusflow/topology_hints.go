package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kube-metrics/nexusflow/pkg/topology"
)

func topologyHintsCmd(args []string) {
	fs := flag.NewFlagSet("topology hints", flag.ExitOnError)
	format := fs.String("format", "shell", "shell | json | openmpi | slurm")
	source := fs.String("source", "auto", "topology source: sysfs, hwloc, auto")
	_ = fs.Parse(args)

	t, srcUsed, err := discoverTopology(*source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology hints: %v\n", err)
		os.Exit(1)
	}
	h := topology.BuildClusterHints(t, srcUsed)

	switch *format {
	case "shell":
		if err := topology.WriteHintsShell(os.Stdout, h); err != nil {
			fmt.Fprintf(os.Stderr, "topology hints: %v\n", err)
			os.Exit(1)
		}
	case "json":
		b, err := topology.FormatHintsJSON(h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "topology hints: %v\n", err)
			os.Exit(1)
		}
		fmt.Println(string(b))
	case "openmpi":
		if err := topology.WriteHintsOpenMPI(os.Stdout, h); err != nil {
			fmt.Fprintf(os.Stderr, "topology hints: %v\n", err)
			os.Exit(1)
		}
	case "slurm":
		if err := topology.WriteHintsSlurm(os.Stdout, h); err != nil {
			fmt.Fprintf(os.Stderr, "topology hints: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "topology hints: unknown --format %q\n", *format)
		os.Exit(2)
	}
}

func topologyMatrixCmd(args []string) {
	fs := flag.NewFlagSet("topology matrix", flag.ExitOnError)
	source := fs.String("source", "auto", "topology source: sysfs, hwloc, auto")
	_ = fs.Parse(args)

	t, srcUsed, err := discoverTopology(*source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology matrix: %v\n", err)
		os.Exit(1)
	}
	h := topology.BuildClusterHints(t, srcUsed)
	fmt.Fprintf(os.Stderr, "# source=%s\n", srcUsed)
	fmt.Print(topology.FormatHintsMatrix(h))
}
