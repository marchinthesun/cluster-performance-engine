//go:build linux

package perf

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Kind selects a hardware perf counter config (PERF_COUNT_HW_*).
type Kind uint64

const (
	KindCPUCycles    Kind = unix.PERF_COUNT_HW_CPU_CYCLES
	KindInstructions Kind = unix.PERF_COUNT_HW_INSTRUCTIONS
)

// Counter wraps a perf_event fd (IPC-ready: dup/send via SCM_RIGHTS).
type Counter struct {
	fd int
}

// Open creates a hardware counter for pid (-1 current) and cpu (-1 any).
func Open(kind Kind, pid int, cpu int) (*Counter, error) {
	attr := unix.PerfEventAttr{
		Type:   unix.PERF_TYPE_HARDWARE,
		Size:   uint32(unsafe.Sizeof(unix.PerfEventAttr{})),
		Config: uint64(kind),
	}
	fd, err := unix.PerfEventOpen(&attr, pid, cpu, -1, 0)
	if err != nil {
		return nil, fmt.Errorf("perf_event_open: %w", err)
	}
	return &Counter{fd: fd}, nil
}

func (c *Counter) Reset() error {
	return unix.IoctlSetInt(c.fd, unix.PERF_EVENT_IOC_RESET, 0)
}

func (c *Counter) Enable() error {
	return unix.IoctlSetInt(c.fd, unix.PERF_EVENT_IOC_ENABLE, 0)
}

func (c *Counter) Disable() error {
	return unix.IoctlSetInt(c.fd, unix.PERF_EVENT_IOC_DISABLE, 0)
}

// ReadUint64 reads the 64-bit counter value from the perf fd.
func (c *Counter) ReadUint64() (uint64, error) {
	var buf [8]byte
	n, err := unix.Read(c.fd, buf[:])
	if err != nil {
		return 0, err
	}
	if n != 8 {
		return 0, fmt.Errorf("perf read: got %d bytes", n)
	}
	return binary.NativeEndian.Uint64(buf[:]), nil
}

func (c *Counter) FD() int { return c.fd }

func (c *Counter) Close() error {
	return unix.Close(c.fd)
}
