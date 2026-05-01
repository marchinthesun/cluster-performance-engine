package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/kube-metrics/nexusflow/pkg/daemon"
	"github.com/kube-metrics/nexusflow/pkg/hugepages"
)

func daemonMain(args []string) {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	listen := fs.String("listen", daemon.DefaultListen, "gRPC TCP listen address")
	cgroup := fs.String("cgroup-root", "", "cgroup v2 base directory (default from $NEXUSFLOW_CGROUP_ROOT or /sys/fs/cgroup/nexusflow-daemon)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	root := *cgroup
	if root == "" {
		root = os.Getenv("NEXUSFLOW_CGROUP_ROOT")
	}
	log.Printf("nexusflow daemon listening on %s", *listen)
	if err := daemon.Serve(*listen, root); err != nil {
		log.Fatal(err)
	}
}

func hugepagesSetCmd(args []string) {
	fs := flag.NewFlagSet("hugepages set", flag.ExitOnError)
	pages := fs.Int("pages", 0, "target nr_hugepages count")
	count := fs.Int("count", -1, "alias for --pages (if set >= 0, wins over --pages)")
	size := fs.String("size", "2M", "2M or 1G")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	n := *pages
	if *count >= 0 {
		n = *count
	}
	v, err := hugepages.SetNrHugepages(*size, n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "hugepages: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("nr_hugepages=%d\n", v)
}
