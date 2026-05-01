//go:build !linux

package affinity

import "fmt"

func Run(_ []int, argv []string, _ RunOpts) error {
	return fmt.Errorf("nexusflow run: CPU pinning only supported on Linux")
}
