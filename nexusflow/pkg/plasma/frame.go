package plasma

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

const maxFrameBytes = 4 << 20 // 4 MiB

// ReadFrameBytes reads a uint32 BE length-prefixed payload.
func ReadFrameBytes(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, err
	}
	ln := binary.BigEndian.Uint32(hdr[:])
	if ln > maxFrameBytes {
		return nil, fmt.Errorf("plasma: frame too large (%d)", ln)
	}
	if ln == 0 {
		return nil, nil
	}
	body := make([]byte, ln)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// WriteJSONFrame writes length-prefixed JSON (no ancillary data).
func WriteJSONFrame(w io.Writer, v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	if _, err := w.Write(hdr[:]); err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
