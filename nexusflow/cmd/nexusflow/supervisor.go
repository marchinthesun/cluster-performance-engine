package main

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kube-metrics/nexusflow/pkg/dashboard"
	"github.com/kube-metrics/nexusflow/pkg/kubermetrics"
	"github.com/kube-metrics/nexusflow/pkg/plasma"
)

func runSupervisor() {
	kubePath := os.Getenv("KUBECONFIG")
	if kubePath == "" {
		kubePath = "/kube/config"
	}
	switch strings.ToLower(strings.TrimSpace(os.Getenv("SKIP_KUBE_DEPLOY"))) {
	case "1", "true", "yes", "on":
		log.Println("nexusflow: SKIP_KUBE_DEPLOY set — skipping manifest apply")
	default:
		if _, err := os.Stat(kubePath); err != nil {
			log.Printf("nexusflow: no kubeconfig at %s — skipping kube deploy", kubePath)
		} else {
			log.Println("nexusflow: kube deploy…")
			kubermetrics.Main(nil)
		}
	}

	sock := envOr("NEXUSFLOW_PLASMA_SOCK", "/run/plasma.sock")
	pipeline := envOr("NEXUSFLOW_PLASMA_FILE", "/demo/plasma.yaml")
	shmName := envOr("NEXUSFLOW_PLASMA_SHM_NAME", "dockdemo")
	shmSize := envInt64("NEXUSFLOW_PLASMA_SHM_SIZE", 65536)
	idle := envDuration("NEXUSFLOW_PLASMA_IDLE_EXIT", 5*time.Minute)
	dashAddr := envOr("NEXUSFLOW_DASHBOARD_LISTEN", "0.0.0.0:9842")

	_ = os.Remove(sock)

	t, _, err := discoverTopology("sysfs")
	if err != nil {
		log.Printf("nexusflow: topology: %v — Plasma coordinator disabled", err)
		t = nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errPlasma := make(chan error, 1)
	go func() {
		if t == nil {
			errPlasma <- nil
			return
		}
		errPlasma <- plasma.Run(ctx, plasma.Config{
			SocketPath:   sock,
			PipelinePath: pipeline,
			ShmName:      shmName,
			ShmSize:      shmSize,
			IdleExit:     idle,
		}, t)
	}()

	if t != nil {
		if !waitSocket(sock, 10*time.Second) {
			log.Println("nexusflow: plasma socket not ready — check logs")
		}
	}

	exe, err := os.Executable()
	if err != nil {
		log.Fatalf("nexusflow: %v", err)
	}
	cfg := dashboard.ServerConfig{
		Addr:            dashAddr,
		NexusflowBinary: exe,
	}
	log.Printf("nexusflow: dashboard http://%s/", dashAddr)
	go func() {
		if err := <-errPlasma; err != nil {
			log.Printf("nexusflow: plasma exited: %v", err)
		}
	}()
	if err := cfg.ListenAndServe(); err != nil {
		log.Fatalf("nexusflow: dashboard: %v", err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt64(k string, def int64) int64 {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

func envDuration(k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}

func waitSocket(path string, maxWait time.Duration) bool {
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		if fi, err := os.Stat(path); err == nil && fi.Mode()&os.ModeSocket != 0 {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}
