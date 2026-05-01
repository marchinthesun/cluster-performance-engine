package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/kube-metrics/nexusflow/pkg/plasma"
)

func plasmaRunCmd(args []string) {
	fs := flag.NewFlagSet("plasma run", flag.ExitOnError)
	sock := fs.String("listen", "/tmp/nexusflow-plasma.sock", "unix domain socket path")
	file := fs.String("file", "", "initial pipeline YAML")
	shmName := fs.String("shm-name", "plasma", "named POSIX shm file nexusflow-{name} under /dev/shm")
	shmSize := fs.Int64("shm-size", 65536, "shared memory segment size (bytes)")
	idleExit := fs.Duration("idle-exit", 15*time.Second, "shutdown when no runnable work and socket quiet for this long")
	_ = fs.Parse(args)
	if *file == "" {
		fmt.Fprintf(os.Stderr, "plasma run: --file required\n")
		os.Exit(2)
	}

	topo, _, err := discoverTopology("sysfs")
	if err != nil {
		fmt.Fprintf(os.Stderr, "topology: %v\n", err)
		os.Exit(1)
	}
	ctx := context.Background()
	err = plasma.Run(ctx, plasma.Config{
		SocketPath:   *sock,
		PipelinePath: *file,
		ShmName:      *shmName,
		ShmSize:      *shmSize,
		IdleExit:     *idleExit,
	}, topo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "plasma: %v\n", err)
		os.Exit(1)
	}
}
