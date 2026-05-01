//go:build linux

package plasma

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"

	"golang.org/x/sys/unix"
)

// WriteJSONFrameWithFD sends one Unix message containing length-prefixed JSON plus SCM_RIGHTS for fd.
// fd is always closed by this function (whether or not send succeeds).
func WriteJSONFrameWithFD(conn *net.UnixConn, v interface{}, fd int) error {
	if fd >= 0 {
		defer unix.Close(fd)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var hdr [4]byte
	binary.BigEndian.PutUint32(hdr[:], uint32(len(b)))
	pkt := append(hdr[:], b...)
	var oob []byte
	if fd >= 0 {
		oob = unix.UnixRights(fd)
	}
	_, _, err = conn.WriteMsgUnix(pkt, oob, nil)
	return err
}

// ReadJSONFrameWithOptionalFD reads one framed JSON blob and optional SCM_RIGHTS fds (recv side).
func ReadJSONFrameWithOptionalFD(conn *net.UnixConn) ([]byte, []int, error) {
	buf := make([]byte, 4+maxFrameBytes)
	oob := make([]byte, unix.CmsgSpace(4*8))
	n, oobn, _, _, err := conn.ReadMsgUnix(buf, oob)
	if err != nil {
		return nil, nil, err
	}
	if n < 4 {
		return nil, nil, fmt.Errorf("plasma: short unix read")
	}
	ln := binary.BigEndian.Uint32(buf[:4])
	if ln > maxFrameBytes {
		return nil, nil, fmt.Errorf("plasma: frame too large")
	}
	end := 4 + int(ln)
	if end > n {
		return nil, nil, fmt.Errorf("plasma: truncated frame")
	}
	body := append([]byte(nil), buf[4:end]...)
	var fds []int
	if oobn > 0 {
		msgs, err := unix.ParseSocketControlMessage(oob[:oobn])
		if err != nil {
			return body, nil, err
		}
		for i := range msgs {
			got, err := unix.ParseUnixRights(&msgs[i])
			if err != nil {
				continue
			}
			fds = append(fds, got...)
		}
	}
	return body, fds, nil
}
