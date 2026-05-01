//go:build !linux

package daemon

import "fmt"

// DefaultListen is ignored on non-Linux builds.
const DefaultListen = "127.0.0.1:50051"

// Serve is unavailable outside Linux.
func Serve(addr, cgroupRoot string) error {
	return fmt.Errorf("nexusflow daemon: Linux only (got stub)")
}
