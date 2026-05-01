package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/kube-metrics/nexusflow/pkg/dashboard"
)

type cidrFlags []string

func (c *cidrFlags) String() string { return strings.Join(*c, ",") }

func (c *cidrFlags) Set(v string) error {
	*c = append(*c, strings.TrimSpace(v))
	return nil
}

func dashboardCmd(args []string) {
	fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
	addr := fs.String("listen", "127.0.0.1:9842", "HTTP listen address (bind all interfaces only with TLS + auth)")
	certFile := fs.String("tls-cert", "", "TLS certificate PEM (requires --tls-key)")
	keyFile := fs.String("tls-key", "", "TLS private key PEM (requires --tls-cert)")
	authTok := fs.String("auth-token", "", "if set, POST /api/exec requires Authorization: Bearer <token>")
	var cidrs cidrFlags
	fs.Var(&cidrs, "allow-cidr", "if set (repeatable), reject requests whose TCP peer is outside these CIDRs (e.g. 10.0.0.0/8)")
	_ = fs.Parse(args)

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
		os.Exit(1)
	}

	cfg := dashboard.ServerConfig{
		Addr:            strings.TrimSpace(*addr),
		NexusflowBinary: exe,
		TLSCertFile:     strings.TrimSpace(*certFile),
		TLSKeyFile:      strings.TrimSpace(*keyFile),
		AuthToken:       strings.TrimSpace(*authTok),
		AllowedCIDRs:    []string(cidrs),
	}

	cf := cfg.TLSCertFile != ""
	kf := cfg.TLSKeyFile != ""
	if cf != kf {
		fmt.Fprintf(os.Stderr, "dashboard: both --tls-cert and --tls-key are required for HTTPS\n")
		os.Exit(2)
	}

	if err := cfg.ListenAndServe(); err != nil {
		fmt.Fprintf(os.Stderr, "dashboard: %v\n", err)
		os.Exit(1)
	}
}
