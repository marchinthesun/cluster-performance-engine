//go:build linux

package plasma

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/kube-metrics/nexusflow/pkg/affinity"
	"github.com/kube-metrics/nexusflow/pkg/dag"
	"github.com/kube-metrics/nexusflow/pkg/perf"
	"github.com/kube-metrics/nexusflow/pkg/shm"
	"github.com/kube-metrics/nexusflow/pkg/topology"
	"golang.org/x/sys/unix"
)

// Run listens on cfg.SocketPath and executes an initial DAG with dynamic branching via unix IPC.
func Run(ctx context.Context, cfg Config, topo *topology.Topology) error {
	if cfg.SocketPath == "" {
		return fmt.Errorf("plasma: socket path required")
	}
	if cfg.PipelinePath == "" {
		return fmt.Errorf("plasma: pipeline file required")
	}
	if topo == nil {
		return fmt.Errorf("plasma: topology required")
	}
	if cfg.IdleExit <= 0 {
		cfg.IdleExit = 15 * time.Second
	}
	if cfg.ShmSize <= 0 {
		cfg.ShmSize = 65536
	}
	if cfg.ShmName == "" {
		cfg.ShmName = "plasma"
	}

	staleShm := filepath.Join("/dev/shm", "nexusflow-"+cfg.ShmName)
	// Prior coordinator runs may leave the backing file; CreateNamed uses O_EXCL.
	log.Printf("plasma: unlink stale shm path %s if present", staleShm)
	if err := os.Remove(staleShm); err != nil && !os.IsNotExist(err) {
		log.Printf("plasma: remove stale shm %s: %v", staleShm, err)
	}

	p, err := dag.LoadFile(cfg.PipelinePath)
	if err != nil {
		return err
	}

	seg, err := shm.CreateNamed(cfg.ShmName, cfg.ShmSize)
	if err != nil {
		return fmt.Errorf("plasma shm: %w", err)
	}

	_ = os.Remove(cfg.SocketPath)
	ln, err := net.Listen("unix", cfg.SocketPath)
	if err != nil {
		_ = seg.Remove()
		return fmt.Errorf("plasma listen: %w", err)
	}
	if err := os.Chmod(cfg.SocketPath, 0666); err != nil {
		log.Printf("plasma: chmod socket: %v", err)
	}

	coord := &coordinator{
		cfg:       cfg,
		topo:      topo,
		ln:        ln,
		nodes:     append([]dag.Node(nil), p.Nodes...),
		completed: map[string]struct{}{},
		running:   map[string]struct{}{},
		wake:      make(chan struct{}, 1),
		seg:       seg,
	}
	coord.bumpActivity()

	go coord.acceptLoop()

	defer func() {
		_ = ln.Close()
		_ = os.Remove(cfg.SocketPath)
		coord.dumpSamples()
		if coord.seg != nil {
			_ = coord.seg.Remove()
		}
	}()

	log.Printf("plasma: coordinator listening on unix:%s shm=%s (%d bytes)", cfg.SocketPath, seg.Path(), seg.Size())
	return coord.loop(ctx)
}

type coordinator struct {
	cfg  Config
	topo *topology.Topology
	ln   net.Listener

	mu            sync.Mutex
	wake          chan struct{}
	nodes         []dag.Node
	completed     map[string]struct{}
	running       map[string]struct{}
	seg           *shm.Segment
	samplesMu     sync.Mutex
	samples       []SampleRecord
	activityMu    sync.Mutex
	lastBump      time.Time
	idleAnnounced bool // log "graph quiescent" once until graph/work resumes
}

func (c *coordinator) bumpActivity() {
	c.activityMu.Lock()
	c.lastBump = time.Now()
	c.activityMu.Unlock()
}

func (c *coordinator) idleSince() time.Duration {
	c.activityMu.Lock()
	defer c.activityMu.Unlock()
	return time.Since(c.lastBump)
}

func (c *coordinator) signalWake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

func (c *coordinator) acceptLoop() {
	for {
		conn, err := c.ln.Accept()
		if err != nil {
			return
		}
		uc, ok := conn.(*net.UnixConn)
		if !ok {
			_ = conn.Close()
			continue
		}
		go c.handleConn(uc)
	}
}

func (c *coordinator) handleConn(uc *net.UnixConn) {
	defer uc.Close()
	for {
		body, err := ReadFrameBytes(uc)
		if err != nil {
			if err != io.EOF {
				log.Printf("plasma conn: read: %v", err)
			}
			return
		}
		if len(body) == 0 {
			continue
		}

		var probe struct {
			Op string `json:"op"`
		}
		if err := json.Unmarshal(body, &probe); err != nil {
			_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: "invalid json"})
			continue
		}

		switch probe.Op {
		case CodeHello:
			_ = WriteJSONFrame(uc, AckEnvelope{Op: CodeAck, OK: true})

		case CodeSample:
			var m msgSample
			if err := json.Unmarshal(body, &m); err != nil {
				_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: err.Error()})
				continue
			}
			c.samplesMu.Lock()
			c.samples = append(c.samples, SampleRecord{
				NodeID:       m.NodeID,
				Cycles:       m.Cycles,
				Instructions: m.Instructions,
				TsNs:         m.TsNs,
			})
			c.samplesMu.Unlock()
			c.bumpActivity()
			c.signalWake()
			_ = WriteJSONFrame(uc, AckEnvelope{Op: CodeAck, OK: true})

		case CodeBranch:
			var m msgBranch
			if err := json.Unmarshal(body, &m); err != nil {
				_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: err.Error()})
				continue
			}
			c.mu.Lock()
			merged, err := AppendDAG(c.nodes, m.Nodes)
			if err != nil {
				c.mu.Unlock()
				_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: err.Error()})
				continue
			}
			c.nodes = merged
			c.idleAnnounced = false
			c.mu.Unlock()
			log.Printf("plasma: branch merged parent=%q new_nodes=%d total_vertices=%d", m.Parent, len(m.Nodes), len(merged))
			c.bumpActivity()
			c.signalWake()
			_ = WriteJSONFrame(uc, AckEnvelope{Op: CodeAck, OK: true})

		case CodeRequestFD:
			var m msgRequestFD
			if err := json.Unmarshal(body, &m); err != nil {
				_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: err.Error()})
				continue
			}
			if err := c.handleRequestFD(uc, &m); err != nil {
				_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: err.Error()})
			}

		default:
			_ = WriteJSONFrame(uc, ErrEnvelope{Op: CodeError, Error: fmt.Sprintf("unknown op %q", probe.Op)})
		}
	}
}

func (c *coordinator) handleRequestFD(uc *net.UnixConn, m *msgRequestFD) error {
	switch m.Kind {
	case "shm":
		f, err := os.OpenFile(c.seg.Path(), os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		dup, err := unix.Dup(int(f.Fd()))
		_ = f.Close()
		if err != nil {
			return err
		}
		reply := MsgFDReply{
			Op:      CodeFDReply,
			Kind:    "shm",
			ShmPath: c.seg.Path(),
			ShmSize: c.seg.Size(),
		}
		c.bumpActivity()
		return WriteJSONFrameWithFD(uc, reply, dup)

	case "perf_cycles":
		pc, err := perf.Open(perf.KindCPUCycles, -1, -1)
		if err != nil {
			return err
		}
		dup, err := unix.Dup(pc.FD())
		_ = pc.Close()
		if err != nil {
			return err
		}
		reply := MsgFDReply{
			Op:             CodeFDReply,
			Kind:           "perf_cycles",
			PerfIOCReset:   uint64(unix.PERF_EVENT_IOC_RESET),
			PerfIOCEnable:  uint64(unix.PERF_EVENT_IOC_ENABLE),
			PerfIOCDisable: uint64(unix.PERF_EVENT_IOC_DISABLE),
		}
		c.bumpActivity()
		return WriteJSONFrameWithFD(uc, reply, dup)

	case "perf_instructions":
		pc, err := perf.Open(perf.KindInstructions, -1, -1)
		if err != nil {
			return err
		}
		dup, err := unix.Dup(pc.FD())
		_ = pc.Close()
		if err != nil {
			return err
		}
		reply := MsgFDReply{
			Op:             CodeFDReply,
			Kind:           "perf_instructions",
			PerfIOCReset:   uint64(unix.PERF_EVENT_IOC_RESET),
			PerfIOCEnable:  uint64(unix.PERF_EVENT_IOC_ENABLE),
			PerfIOCDisable: uint64(unix.PERF_EVENT_IOC_DISABLE),
		}
		c.bumpActivity()
		return WriteJSONFrameWithFD(uc, reply, dup)

	default:
		return fmt.Errorf("unknown fd kind %q", m.Kind)
	}
}

func (c *coordinator) execNode(ctx context.Context, n *dag.Node) error {
	cpus, err := topology.SelectCPUs(c.topo, n.CPUs, n.NUMANode, topology.StrategySameNUMA)
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
	cmd.Env = append(os.Environ(),
		"NEXUSFLOW_PLASMA_SOCK="+c.cfg.SocketPath,
		"NEXUSFLOW_NODE_ID="+n.ID,
		"NEXUSFLOW_SHM_PATH="+c.seg.Path(),
		fmt.Sprintf("NEXUSFLOW_SHM_SIZE=%d", c.seg.Size()),
	)

	log.Printf("plasma: exec node=%q cpus=%v cmd=%v", n.ID, cpus, n.Cmd)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("node %q: %w", n.ID, err)
	}
	c.bumpActivity()
	return nil
}

func (c *coordinator) loop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		c.mu.Lock()
		next := PickRunnable(c.nodes, c.completed, c.running)
		runningCnt := len(c.running)
		c.mu.Unlock()

		if next != nil {
			c.mu.Lock()
			c.running[next.ID] = struct{}{}
			c.mu.Unlock()

			err := c.execNode(ctx, next)

			c.mu.Lock()
			delete(c.running, next.ID)
			if err != nil {
				c.mu.Unlock()
				return err
			}
			c.completed[next.ID] = struct{}{}
			c.idleAnnounced = false
			total := len(c.nodes)
			done := len(c.completed)
			c.mu.Unlock()

			log.Printf("plasma: completed node=%q (%d/%d vertices)", next.ID, done, total)
			c.signalWake()
			continue
		}

		if runningCnt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-c.wake:
			case <-time.After(50 * time.Millisecond):
			}
			continue
		}

		c.mu.Lock()
		st := len(c.running)
		total := len(c.nodes)
		done := len(c.completed)
		if st == 0 && total > 0 && done == total && !c.idleAnnounced {
			log.Printf("plasma: graph quiescent (%d vertices completed); idle-exit in %v if no new branches/samples", total, c.cfg.IdleExit)
			c.idleAnnounced = true
		}
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.wake:
			continue
		case <-time.After(250 * time.Millisecond):
			if c.idleSince() >= c.cfg.IdleExit && runningCnt == 0 {
				log.Printf("plasma: idle timeout (%v), shutting down", c.cfg.IdleExit)
				return nil
			}
		}
	}
}

func (c *coordinator) dumpSamples() {
	c.samplesMu.Lock()
	defer c.samplesMu.Unlock()
	if len(c.samples) == 0 {
		log.Printf("plasma: no samples collected")
		return
	}
	log.Printf("plasma: sample log (%d entries)", len(c.samples))
	for _, s := range c.samples {
		b, err := json.Marshal(s)
		if err != nil {
			continue
		}
		fmt.Fprintf(os.Stderr, "%s\n", string(b))
	}
}
