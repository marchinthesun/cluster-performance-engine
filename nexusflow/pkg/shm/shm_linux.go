//go:build linux

package shm

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"golang.org/x/sys/unix"
)

var shmNamePat = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,126}$`)

// Segment is a POSIX shared-memory file under /dev/shm mapped with MAP_SHARED.
type Segment struct {
	path string
	size int64
	data []byte
}

// Create allocates a uniquely named segment in /dev/shm and mmap-s it RW.
// Call Close() to munmap; call Remove() when all peers are done to unlink.
func Create(size int64) (*Segment, error) {
	if size <= 0 {
		return nil, fmt.Errorf("shm: invalid size %d", size)
	}
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return nil, err
	}
	name := "nexusflow-" + hex.EncodeToString(rnd[:])
	shmPath := filepath.Join("/dev/shm", name)

	f, err := os.OpenFile(shmPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("shm open %s: %w", shmPath, err)
	}
	fd := int(f.Fd())
	if err := unix.Ftruncate(fd, size); err != nil {
		f.Close()
		os.Remove(shmPath)
		return nil, fmt.Errorf("shm ftruncate: %w", err)
	}

	reg, err := unix.Mmap(fd, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		os.Remove(shmPath)
		return nil, fmt.Errorf("shm mmap: %w", err)
	}
	if err := f.Close(); err != nil {
		unix.Munmap(reg)
		os.Remove(shmPath)
		return nil, err
	}

	return &Segment{path: shmPath, size: size, data: reg}, nil
}

// CreateNamed allocates `/dev/shm/nexusflow-{name}` exclusively (ASCII slug).
func CreateNamed(name string, size int64) (*Segment, error) {
	if size <= 0 {
		return nil, fmt.Errorf("shm: invalid size %d", size)
	}
	if !shmNamePat.MatchString(name) {
		return nil, fmt.Errorf("shm: invalid name %q (use letters, digits, ._-)", name)
	}
	shmPath := filepath.Join("/dev/shm", "nexusflow-"+name)

	f, err := os.OpenFile(shmPath, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, fmt.Errorf("shm open %s: %w", shmPath, err)
	}
	fd := int(f.Fd())
	if err := unix.Ftruncate(fd, size); err != nil {
		f.Close()
		os.Remove(shmPath)
		return nil, fmt.Errorf("shm ftruncate: %w", err)
	}

	reg, err := unix.Mmap(fd, 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		os.Remove(shmPath)
		return nil, fmt.Errorf("shm mmap: %w", err)
	}
	if err := f.Close(); err != nil {
		unix.Munmap(reg)
		os.Remove(shmPath)
		return nil, err
	}

	return &Segment{path: shmPath, size: size, data: reg}, nil
}

// OpenPath mmap-s an existing /dev/shm path (must exist).
func OpenPath(shmPath string, size int64) (*Segment, error) {
	f, err := os.OpenFile(shmPath, os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	reg, err := unix.Mmap(int(f.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}
	return &Segment{path: shmPath, size: size, data: reg}, nil
}

func (s *Segment) Path() string { return s.path }
func (s *Segment) Size() int64  { return s.size }
func (s *Segment) Bytes() []byte {
	return s.data
}

// Close munmaps the mapping (backing file stays until Remove).
func (s *Segment) Close() error {
	if len(s.data) == 0 {
		return nil
	}
	err := unix.Munmap(s.data)
	s.data = nil
	return err
}

// Remove deletes the backing file in /dev/shm (unmap first if needed).
func (s *Segment) Remove() error {
	if len(s.data) > 0 {
		if err := unix.Munmap(s.data); err != nil {
			return err
		}
		s.data = nil
	}
	if s.path != "" {
		err := os.Remove(s.path)
		s.path = ""
		return err
	}
	return nil
}
