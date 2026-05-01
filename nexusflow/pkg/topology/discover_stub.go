//go:build !linux

package topology

import "fmt"

// Discover is only implemented on Linux (sysfs).
func Discover() (*Topology, error) {
	return nil, fmt.Errorf("nexusflow topology: only Linux is supported (requires /sys/devices/system/cpu)")
}
