package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kube-metrics/nexusflow/pkg/dag"
	"github.com/kube-metrics/nexusflow/pkg/hwloc"
	"github.com/kube-metrics/nexusflow/pkg/perf"
	"github.com/kube-metrics/nexusflow/pkg/shm"
	"github.com/kube-metrics/nexusflow/pkg/topology"
)

func discoverTopology(mode string) (*topology.Topology, string, error) {
	switch mode {
	case "hwloc":
		t, err := hwloc.DiscoverCLI()
		return t, "hwloc-xml", err
	case "sysfs":
		t, err := topology.Discover()
		return t, "sysfs", err
	default:
		t, err := topology.Discover()
		if err == nil {
			return t, "sysfs", nil
		}
		th, err2 := hwloc.DiscoverCLI()
		if err2 == nil {
			return th, "hwloc-xml(fallback)", nil
		}
		return nil, "", fmt.Errorf("topology auto: sysfs: %v; hwloc: %w", err, err2)
	}
}

func dagRunCmd(args []string) {
	fs := flag.NewFlagSet("dag run", flag.ExitOnError)
	file := fs.String("file", "", "pipeline YAML path")
	promPath := fs.String("prom-file", "", "after run, write Prometheus text metrics (DAG step duration & exit_code)")
	_ = fs.Parse(args)
	if *file == "" {
		fmt.Fprintf(os.Stderr, "dag run: --file required\n")
		os.Exit(2)
	}
	p, err := dag.LoadFile(*file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "dag: %v\n", err)
		os.Exit(1)
	}
	topo, _, err := discoverTopology("sysfs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	r := dag.Runner{Topo: topo, PromTextFile: strings.TrimSpace(*promPath)}
	if err := r.Run(ctx, p); err != nil {
		fmt.Fprintf(os.Stderr, "dag run: %v\n", err)
		os.Exit(1)
	}
}

func shmCreateCmd(args []string) {
	fs := flag.NewFlagSet("shm create", flag.ExitOnError)
	sizeStr := fs.String("size", "4096", "segment size in bytes (decimal)")
	name := fs.String("name", "", "optional fixed slug for /dev/shm/nexusflow-{name} (omit for random)")
	_ = fs.Parse(args)
	size, err := strconv.ParseInt(*sizeStr, 10, 64)
	if err != nil || size <= 0 {
		fmt.Fprintf(os.Stderr, "shm create: bad --size\n")
		os.Exit(2)
	}
	var seg *shm.Segment
	if strings.TrimSpace(*name) != "" {
		seg, err = shm.CreateNamed(strings.TrimSpace(*name), size)
	} else {
		seg, err = shm.Create(size)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "shm: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "nexusflow shm: segment ready (unmap closes mapping; rm file when finished)\n")
	fmt.Printf("NEXUSFLOW_SHM_PATH=%s\n", seg.Path())
	fmt.Printf("NEXUSFLOW_SHM_SIZE=%d\n", seg.Size())
	if err := seg.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "shm munmap: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "hint: rm %s after peers detach\n", seg.Path())
}

func perfSampleCmd(args []string) {
	fs := flag.NewFlagSet("perf sample", flag.ExitOnError)
	sleepMs := fs.Int("sleep-ms", 100, "measurement window length")
	kindStr := fs.String("kind", "cycles", "cycles|instructions")
	_ = fs.Parse(args)

	var kind perf.Kind
	switch *kindStr {
	case "cycles":
		kind = perf.KindCPUCycles
	case "instructions":
		kind = perf.KindInstructions
	default:
		fmt.Fprintf(os.Stderr, "perf sample: unknown --kind\n")
		os.Exit(2)
	}

	c, err := perf.Open(kind, -1, -1)
	if err != nil {
		fmt.Fprintf(os.Stderr, "perf: %v\n", err)
		os.Exit(1)
	}
	defer c.Close()

	if err := c.Reset(); err != nil {
		fmt.Fprintf(os.Stderr, "perf reset: %v\n", err)
		os.Exit(1)
	}
	if err := c.Enable(); err != nil {
		fmt.Fprintf(os.Stderr, "perf enable: %v\n", err)
		os.Exit(1)
	}
	time.Sleep(time.Duration(*sleepMs) * time.Millisecond)
	if err := c.Disable(); err != nil {
		fmt.Fprintf(os.Stderr, "perf disable: %v\n", err)
		os.Exit(1)
	}
	v, err := c.ReadUint64()
	if err != nil {
		fmt.Fprintf(os.Stderr, "perf read: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "nexusflow perf: fd=%d (dup/send via SCM_RIGHTS for IPC)\n", c.FD())
	fmt.Printf("%s=%d\n", *kindStr, v)
}
