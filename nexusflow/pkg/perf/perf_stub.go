//go:build !linux

package perf

import "fmt"

type Kind uint64

const (
	KindCPUCycles Kind = iota
	KindInstructions
)

type Counter struct{}

func Open(Kind, int, int) (*Counter, error) {
	return nil, fmt.Errorf("perf: only supported on Linux")
}

func (*Counter) Reset() error                   { return nil }
func (*Counter) Enable() error                  { return nil }
func (*Counter) Disable() error                 { return nil }
func (*Counter) ReadUint64() (uint64, error)    { return 0, fmt.Errorf("perf: stub") }
func (*Counter) FD() int                       { return -1 }
func (*Counter) Close() error                   { return nil }
