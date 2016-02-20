// +build linux,darwin
package ptp

import (
	"encoding/binary"
	"io"
	"os"
	"unsafe"
)

type Interface struct {
	name string
	file *os.File
	meta bool
}

// Read a single packet from the kernel.
func (t *Interface) ReadPacket() (*Packet, error) {
	buf := make([]byte, 10000)

	n, err := t.file.Read(buf)
	if err != nil {
		return nil, err
	}

	var pkt *Packet
	if t.meta {
		pkt = &Packet{Packet: buf[4:n]}
	} else {
		pkt = &Packet{Packet: buf[0:n]}
	}
	pkt.Protocol = int(binary.BigEndian.Uint16(buf[2:4]))
	flags := int(*(*uint16)(unsafe.Pointer(&buf[0])))
	if flags&flagTruncated != 0 {
		pkt.Truncated = true
	}
	return pkt, nil
}

// Send a single packet to the kernel.
func (t *Interface) WritePacket(pkt *Packet) error {
	// If only we had writev(), I could do zero-copy here...
	buf := make([]byte, len(pkt.Packet)+4)
	binary.BigEndian.PutUint16(buf[2:4], uint16(pkt.Protocol))
	copy(buf[4:], pkt.Packet)

	var n int
	var err error
	if t.meta {
		n, err = t.file.Write(buf)
	} else {
		n, err = t.file.Write(pkt.Packet)
	}
	if err != nil {
		return err
	}
	if n != len(buf) {
		return io.ErrShortWrite
	}
	return nil
}
