//go:build linux

package daemon

import (
	"context"
	"net"
	"os"
	"os/exec"
	"sync"
	"time"

	nexusflowv1 "github.com/kube-metrics/nexusflow/api/v1"
	"github.com/kube-metrics/nexusflow/pkg/cgv2"
	"github.com/kube-metrics/nexusflow/pkg/evict"
	"github.com/kube-metrics/nexusflow/pkg/hugepages"
	"github.com/kube-metrics/nexusflow/pkg/perf"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements NexusFlowDaemon gRPC API (cgroup v2 cells, LLC sampling, hugepages, eviction).
type Server struct {
	nexusflowv1.UnimplementedNexusFlowDaemonServer
	mu    sync.RWMutex
	cg    *cgv2.Manager
	cells map[string]*cellMeta
}

type cellMeta struct {
	Path string
	CPUs []int32
}

// NewServer builds a cgroup-backed daemon service.
func NewServer(cgroupRoot string) (*Server, error) {
	cg, err := cgv2.NewManager(cgroupRoot)
	if err != nil {
		return nil, err
	}
	return &Server{
		cg:    cg,
		cells: make(map[string]*cellMeta),
	}, nil
}

func (s *Server) CreateCell(ctx context.Context, req *nexusflowv1.CreateCellRequest) (*nexusflowv1.CreateCellReply, error) {
	if req.CellId == "" || len(req.Cpus) == 0 {
		return nil, status.Error(codes.InvalidArgument, "cell_id and cpus required")
	}
	path, err := s.cg.CreateCell(req.CellId, req.Cpus, req.Mems, req.Exclusive)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "create cell: %v", err)
	}
	s.mu.Lock()
	s.cells[req.CellId] = &cellMeta{Path: path, CPUs: append([]int32(nil), req.Cpus...)}
	s.mu.Unlock()
	return &nexusflowv1.CreateCellReply{CgroupPath: path}, nil
}

func (s *Server) DestroyCell(ctx context.Context, req *nexusflowv1.DestroyCellRequest) (*nexusflowv1.DestroyCellReply, error) {
	s.mu.Lock()
	delete(s.cells, req.CellId)
	s.mu.Unlock()
	if err := s.cg.DestroyCell(req.CellId); err != nil {
		return nil, status.Errorf(codes.Internal, "destroy: %v", err)
	}
	return &nexusflowv1.DestroyCellReply{}, nil
}

func (s *Server) AttachPID(ctx context.Context, req *nexusflowv1.AttachPIDRequest) (*nexusflowv1.AttachPIDReply, error) {
	s.mu.RLock()
	meta, ok := s.cells[req.CellId]
	s.mu.RUnlock()
	if !ok {
		return nil, status.Errorf(codes.NotFound, "cell %q", req.CellId)
	}
	if err := cgv2.Attach(meta.Path, int(req.Pid)); err != nil {
		return nil, status.Errorf(codes.Internal, "attach: %v", err)
	}
	return &nexusflowv1.AttachPIDReply{}, nil
}

func (s *Server) RunInCell(req *nexusflowv1.RunInCellRequest, stream nexusflowv1.NexusFlowDaemon_RunInCellServer) error {
	s.mu.RLock()
	meta, ok := s.cells[req.CellId]
	s.mu.RUnlock()
	if !ok {
		return status.Errorf(codes.NotFound, "cell %q", req.CellId)
	}
	if len(req.Argv) == 0 {
		return status.Error(codes.InvalidArgument, "empty argv")
	}
	ctx := stream.Context()
	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	or, ow, err := os.Pipe()
	if err != nil {
		return err
	}
	er, ew, err := os.Pipe()
	if err != nil {
		ow.Close()
		or.Close()
		return err
	}
	cmd.Stdout = ow
	cmd.Stderr = ew
	if err := cmd.Start(); err != nil {
		ow.Close()
		ew.Close()
		or.Close()
		er.Close()
		return err
	}
	ow.Close()
	ew.Close()
	_ = cgv2.Attach(meta.Path, cmd.Process.Pid)

	var sendMu sync.Mutex
	send := func(chunk *nexusflowv1.RunInCellChunk) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = stream.Send(chunk)
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer or.Close()
		pipeToStream(send, or, true)
	}()
	go func() {
		defer wg.Done()
		defer er.Close()
		pipeToStream(send, er, false)
	}()
	wg.Wait()
	waitErr := cmd.Wait()
	exit := int32(0)
	if waitErr != nil {
		if x, ok := waitErr.(*exec.ExitError); ok {
			exit = int32(x.ExitCode())
		} else {
			send(&nexusflowv1.RunInCellChunk{Stderr: []byte(waitErr.Error() + "\n"), ExitCode: -1, Finished: true})
			return nil
		}
	}
	send(&nexusflowv1.RunInCellChunk{ExitCode: exit, Finished: true})
	return nil
}

func pipeToStream(send func(*nexusflowv1.RunInCellChunk), r *os.File, stdout bool) {
	buf := make([]byte, 8192)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			b := append([]byte(nil), buf[:n]...)
			if stdout {
				send(&nexusflowv1.RunInCellChunk{Stdout: b})
			} else {
				send(&nexusflowv1.RunInCellChunk{Stderr: b})
			}
		}
		if err != nil {
			return
		}
	}
}

func (s *Server) WatchL3(req *nexusflowv1.WatchL3Request, stream nexusflowv1.NexusFlowDaemon_WatchL3Server) error {
	s.mu.RLock()
	meta, ok := s.cells[req.CellId]
	s.mu.RUnlock()
	if !ok {
		return status.Errorf(codes.NotFound, "cell %q", req.CellId)
	}
	if len(meta.CPUs) == 0 {
		return status.Error(codes.FailedPrecondition, "cell has no cpus")
	}
	cpu := int(meta.CPUs[0])
	interval := time.Duration(req.IntervalMs) * time.Millisecond
	if interval < 50*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		acc, errA := perf.OpenLLCLastLevelAccess(-1, cpu)
		miss, errM := perf.OpenLLCLastLevelMiss(-1, cpu)
		if errA != nil || errM != nil {
			if acc != nil {
				acc.Close()
			}
			if miss != nil {
				miss.Close()
			}
			return status.Errorf(codes.Internal, "perf llc: access=%v miss=%v", errA, errM)
		}
		_ = acc.Reset()
		_ = miss.Reset()
		_ = acc.Enable()
		_ = miss.Enable()
		time.Sleep(interval)
		_ = acc.Disable()
		_ = miss.Disable()
		av, _ := acc.ReadUint64()
		mv, _ := miss.ReadUint64()
		acc.Close()
		miss.Close()
		if err := stream.Send(&nexusflowv1.L3Sample{
			LlcReferences: av,
			LlcMisses:     mv,
			TimestampUnix: time.Now().Unix(),
		}); err != nil {
			return err
		}
	}
}

func (s *Server) SetHugepages(ctx context.Context, req *nexusflowv1.SetHugepagesRequest) (*nexusflowv1.SetHugepagesReply, error) {
	ps := req.Pagesize
	if ps == "" {
		ps = "2M"
	}
	v, err := hugepages.SetNrHugepages(ps, int(req.Pages))
	if err != nil {
		return nil, status.Errorf(codes.PermissionDenied, "hugepages: %v", err)
	}
	return &nexusflowv1.SetHugepagesReply{ReservedPages: int32(v)}, nil
}

func (s *Server) EvictForeign(ctx context.Context, req *nexusflowv1.EvictForeignRequest) (*nexusflowv1.EvictForeignReply, error) {
	if len(req.Cpus) == 0 {
		return nil, status.Error(codes.InvalidArgument, "cpus required")
	}
	t := evict.NewCPUTarget(req.Cpus)
	sig := unix.SIGSTOP
	if req.SignalNumber != 0 {
		sig = unix.Signal(req.SignalNumber)
	}
	skip := map[int]struct{}{os.Getpid(): {}}
	n, err := evict.StopForeignOnCPUs(t, skip, sig, req.DryRun)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "evict: %v", err)
	}
	return &nexusflowv1.EvictForeignReply{ProcessesStopped: int32(n)}, nil
}

// DefaultListen is the default gRPC TCP address.
const DefaultListen = "127.0.0.1:50051"

// Serve listens on addr and blocks serving the gRPC API.
func Serve(addr, cgroupRoot string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv, err := NewServer(cgroupRoot)
	if err != nil {
		return err
	}
	g := grpc.NewServer()
	nexusflowv1.RegisterNexusFlowDaemonServer(g, srv)
	return g.Serve(lis)
}
