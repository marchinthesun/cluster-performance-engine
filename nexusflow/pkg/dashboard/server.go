package dashboard

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os/exec"
	"strings"
	"time"
)

var allowedRoots = map[string]struct{}{
	"topology": {}, "topo": {}, "run": {}, "dag": {}, "shm": {}, "perf": {}, "plasma": {}, "hugepages": {}, "help": {}, "-h": {}, "--help": {},
}

const maxArgs = 48
const maxArgLen = 4096
const execTimeoutDefault = 3 * time.Minute
const execTimeoutMinSec = 10
const execTimeoutMaxSec = 7200

func execDuration(timeoutSec int) time.Duration {
	if timeoutSec <= 0 {
		return execTimeoutDefault
	}
	if timeoutSec < execTimeoutMinSec {
		timeoutSec = execTimeoutMinSec
	}
	if timeoutSec > execTimeoutMaxSec {
		timeoutSec = execTimeoutMaxSec
	}
	return time.Duration(timeoutSec) * time.Second
}

// ExecRequest is JSON body for POST /api/exec .
type ExecRequest struct {
	Argv       []string `json:"argv"`
	TimeoutSec int      `json:"timeout_sec,omitempty"` // subprocess wall clock (omit or 0 → server default 3m); clamped 10–7200
}

// ExecResponse returns CLI stdout/stderr and exit metadata.
type ExecResponse struct {
	Stdout            string `json:"stdout"`
	Stderr            string `json:"stderr"`
	ExitCode          int    `json:"exit_code"`
	DurationMs        int64  `json:"duration_ms"`
	Error             string `json:"error,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	ServerTimeRFC3339 string `json:"server_time,omitempty"`
}

// ServerConfig binds the dashboard HTTP(S) server with optional TLS, Bearer auth on /api/exec, and IP allow-lists.
type ServerConfig struct {
	Addr            string
	NexusflowBinary string
	TLSCertFile     string
	TLSKeyFile      string
	AuthToken       string
	AllowedCIDRs    []string
}

func newRequestID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("nf%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func validateArgv(argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty argv")
	}
	if len(argv) > maxArgs {
		return fmt.Errorf("too many args (max %d)", maxArgs)
	}
	root := argv[0]
	if _, ok := allowedRoots[root]; !ok {
		return fmt.Errorf("subcommand %q not allowed", root)
	}
	for _, a := range argv {
		if len(a) > maxArgLen {
			return errors.New("argument too long")
		}
		if strings.ContainsRune(a, '\x00') {
			return errors.New("invalid argument")
		}
	}
	return nil
}

func execHandler(binary string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req ExecRequest
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		rid := newRequestID()
		w.Header().Set("X-Request-ID", rid)
		ts := time.Now().UTC().Format(time.RFC3339Nano)
		if err := validateArgv(req.Argv); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(ExecResponse{
				Error:             err.Error(),
				ExitCode:          -1,
				RequestID:         rid,
				ServerTimeRFC3339: ts,
			})
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), execDuration(req.TimeoutSec))
		defer cancel()

		cmd := exec.CommandContext(ctx, binary, req.Argv...)
		var outb, errb bytes.Buffer
		cmd.Stdout = &outb
		cmd.Stderr = &errb

		t0 := time.Now()
		runErr := cmd.Run()
		ms := time.Since(t0).Milliseconds()

		exit := 0
		if runErr != nil {
			if ee, ok := runErr.(*exec.ExitError); ok {
				exit = ee.ExitCode()
			} else {
				exit = -1
			}
		}

		resp := ExecResponse{
			Stdout:            outb.String(),
			Stderr:            errb.String(),
			ExitCode:          exit,
			DurationMs:        ms,
			RequestID:         rid,
			ServerTimeRFC3339: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if runErr != nil && exit == -1 {
			resp.Error = runErr.Error()
		}

		w.Header().Set("Content-Type", "application/json")
		enc := json.NewEncoder(w)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(resp)
	}
}

func parseAllowedNetworks(cidrs []string) ([]*net.IPNet, error) {
	var out []*net.IPNet
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, n, err := net.ParseCIDR(raw)
		if err != nil {
			return nil, fmt.Errorf("dashboard allow-cidr %q: %w", raw, err)
		}
		out = append(out, n)
	}
	return out, nil
}

func ipAllowMiddleware(allowed []*net.IPNet, next http.Handler) http.Handler {
	if len(allowed) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ip := net.ParseIP(host)
		if ip == nil {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		ok := false
		for _, n := range allowed {
			if n.Contains(ip) {
				ok = true
				break
			}
		}
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerAuthMiddleware(token string, next http.Handler) http.Handler {
	token = strings.TrimSpace(token)
	if token == "" {
		return next
	}
	want := "Bearer " + token
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/exec" && r.Method == http.MethodPost {
			if r.Header.Get("Authorization") != want {
				w.Header().Set("WWW-Authenticate", `Bearer realm="nexusflow", charset="UTF-8"`)
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ListenAndServe binds addr and serves the dashboard + /api/exec (no TLS / ACL).
func ListenAndServe(addr, nexusflowBinary string) error {
	cfg := ServerConfig{Addr: addr, NexusflowBinary: nexusflowBinary}
	return cfg.ListenAndServe()
}

// ListenAndServe starts HTTP or HTTPS based on TLSCertFile / TLSKeyFile.
func (cfg ServerConfig) ListenAndServe() error {
	if cfg.Addr == "" {
		cfg.Addr = "127.0.0.1:9842"
	}
	nets, err := parseAllowedNetworks(cfg.AllowedCIDRs)
	if err != nil {
		return err
	}

	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		return err
	}

	health := healthHandler()
	mux := http.NewServeMux()
	mux.Handle("/healthz", health)
	mux.Handle("/health", health)
	mux.HandleFunc("/api/exec", execHandler(cfg.NexusflowBinary))
	mux.Handle("/", http.FileServer(http.FS(sub)))

	handler := http.Handler(mux)
	handler = bearerAuthMiddleware(cfg.AuthToken, handler)
	handler = ipAllowMiddleware(nets, handler)
	handler = logRequests(handler)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	useTLS := strings.TrimSpace(cfg.TLSCertFile) != "" && strings.TrimSpace(cfg.TLSKeyFile) != ""
	if useTLS {
		log.Printf("nexusflow dashboard listening on https://%s", cfg.Addr)
		return srv.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
	}
	log.Printf("nexusflow dashboard listening on http://%s", cfg.Addr)
	return srv.ListenAndServe()
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		p := r.URL.Path
		if p == "/healthz" || p == "/health" {
			return
		}
		log.Printf("%s %s %s", r.Method, p, time.Since(start))
	})
}
