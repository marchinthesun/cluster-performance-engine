package plasma

import "github.com/kube-metrics/nexusflow/pkg/dag"

const (
	CodeHello     = "hello"
	CodeSample    = "sample"
	CodeBranch    = "branch"
	CodeRequestFD = "request_fd"
	CodeAck       = "ack"
	CodeError     = "error"
	CodeFDReply   = "fd_reply"
)

// SampleRecord is one perf-ish observation pushed over the socket (JSON frame).
type SampleRecord struct {
	NodeID       string  `json:"node_id"`
	Cycles       *uint64 `json:"cycles,omitempty"`
	Instructions *uint64 `json:"instructions,omitempty"`
	TsNs         int64   `json:"ts_ns,omitempty"`
}

type msgSample struct {
	Op           string  `json:"op"`
	NodeID       string  `json:"node_id"`
	Cycles       *uint64 `json:"cycles,omitempty"`
	Instructions *uint64 `json:"instructions,omitempty"`
	TsNs         int64   `json:"ts_ns,omitempty"`
}

type msgBranch struct {
	Op     string     `json:"op"`
	Parent string     `json:"parent,omitempty"`
	Nodes  []dag.Node `json:"nodes"`
}

type msgRequestFD struct {
	Op   string `json:"op"`
	Kind string `json:"kind"` // shm | perf_cycles | perf_instructions
}

// MsgFDReply precedes SCM_RIGHTS attachment on the same Unix datagram when Kind requests an FD.
type MsgFDReply struct {
	Op    string `json:"op"`
	Kind  string `json:"kind"`
	Error string `json:"error,omitempty"`

	ShmPath string `json:"shm_path,omitempty"`
	ShmSize int64  `json:"shm_size,omitempty"`

	PerfIOCReset   uint64 `json:"perf_ioc_reset,omitempty"`
	PerfIOCEnable  uint64 `json:"perf_ioc_enable,omitempty"`
	PerfIOCDisable uint64 `json:"perf_ioc_disable,omitempty"`
}

type AckEnvelope struct {
	Op string `json:"op"`
	OK bool   `json:"ok"`
}

type ErrEnvelope struct {
	Op    string `json:"op"`
	Error string `json:"error"`
}
