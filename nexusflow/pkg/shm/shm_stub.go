//go:build !linux

package shm

import "fmt"

type Segment struct{}

func Create(int64) (*Segment, error) {
	return nil, fmt.Errorf("shm: only supported on Linux")
}

func CreateNamed(string, int64) (*Segment, error) {
	return nil, fmt.Errorf("shm: only supported on Linux")
}

func OpenPath(string, int64) (*Segment, error) {
	return nil, fmt.Errorf("shm: only supported on Linux")
}

func (*Segment) Path() string      { return "" }
func (*Segment) Size() int64       { return 0 }
func (*Segment) Bytes() []byte     { return nil }
func (*Segment) Close() error      { return nil }
func (*Segment) Remove() error     { return nil }
