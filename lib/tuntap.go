package ptp

import "net"

const (
	flagTruncated = 0x1
	iffTun        = 0x1
	iffTap        = 0x2
	iffOneQueue   = 0x2000
	iffnopi       = 0x1000
)

type ifReq struct {
	Name  [0x10]byte
	Flags uint16
	pad   [0x28 - 0x10 - 2]byte
}

// Packet represents a packet received on TUN/TAP interface
type Packet struct {
	Protocol int
	Packet   []byte
}

// TAP interface
type TAP interface {
	GetName() string
	GetHardwareAddress() net.HardwareAddr
	GetIP() net.IP
	GetMask() net.IPMask
	GetBasename() string
	SetName(string)
	SetHardwareAddress(net.HardwareAddr)
	SetIP(net.IP)
	SetMask(net.IPMask)
	Init(string) error
	Open() error
	Close() error
	Configure() error
	ReadPacket() (*Packet, error)
	WritePacket(*Packet) error
	Run()
}
