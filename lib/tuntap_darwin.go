// +build darwin
package ptp

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
)

const (
	ConfigDir  string = "/usr/local/etc"
	DefaultMTU int    = 1376
)

// func openDevice(ifPattern string) (*os.File, error) {
// 	file, err := os.OpenFile("/dev/"+ifPattern, os.O_RDWR, 0)
// 	return file, err
// }

// func createInterface(file *os.File, ifPattern string, kind DevKind) (string, error) {
// 	return "1", nil
// }

// func closeInterface(file *os.File) {
// 	Log(Info, "Closing network interface")
// 	if file != nil {
// 		err := file.Close()
// 		if err != nil {
// 			Log(Error, "Failed to close network interface: %s", err)
// 			return
// 		}
// 		Log(Info, "Interface closed")
// 		return
// 	}
// 	Log(Warning, "Skipping previously closed interface")
// }

// func ConfigureInterface(dev *Interface, ip, mac, device, tool string) error {
// 	// First we need to set MAC address, because ifconfig requires interface to go down
// 	// before changing it
// 	setmac := exec.Command(tool, device, "ether", mac)
// 	err := setmac.Run()
// 	if err != nil {
// 		Log(Error, "Failed to set MAC: %v", err)
// 	}
// 	linkup := exec.Command(tool, device, ip, "netmask", "255.255.255.0", "up")
// 	err = linkup.Run()
// 	if err != nil {
// 		Log(Error, "Failed to up link: %v", err)
// 		return err
// 	}
// 	return nil
// }

// func LinkUp(device, tool string) error {
// 	linkup := exec.Command(tool, "link", "set", "dev", device, "up")
// 	err := linkup.Run()
// 	if err != nil {
// 		Log(Error, "Failed to up link: %v", err)
// 		return err
// 	}
// 	return nil
// }

// func SetIp(ip, device, tool string) error {
// 	Log(Info, "Setting %s IP on device %s", ip, device)
// 	setip := exec.Command(tool, "addr", "add", ip+"/24", "dev", device)
// 	err := setip.Run()
// 	if err != nil {
// 		Log(Error, "Failed to set IP: %v", err)
// 		return err
// 	}
// 	return err
// }

// func SetMac(mac, device, tool string) error {
// 	// Set MAC to device
// 	Log(Info, "Setting %s MAC on device %s", mac, device)
// 	setmac := exec.Command(tool, "link", "set", "dev", device, "address", mac)
// 	err := setmac.Run()
// 	if err != nil {
// 		Log(Error, "Failed to set MAC: %v", err)
// 		return err
// 	}
// 	return err
// }

// func GetDeviceBase() string {
// 	return "tap"
// }

func GetDeviceBase() string {
	return "vptp"
}

// GetConfigurationTool function will return path to configuration tool on specific platform
func GetConfigurationTool() string {
	path, err := exec.LookPath("ifconfig")
	if err != nil {
		Log(Error, "Failed to find `ifconfig` in path. Returning default /sbin/ifconfig")
		return "/sbin/ifconfig"
	}
	Log(Info, "Network configuration tool found: %s", path)
	return path
}

func newTAP(tool, ip, mac, mask string, mtu int) (*TAPDarwin, error) {
	Log(Info, "Acquiring TAP interface [Darwin]")
	nip := net.ParseIP(ip)
	if nip == nil {
		return nil, fmt.Errorf("Failed to parse IP during TAP creation")
	}
	nmac, err := net.ParseMAC(mac)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse MAC during TAP creation: %s", err)
	}
	return &TAPDarwin{
		Tool: tool,
		IP:   nip,
		Mac:  nmac,
		Mask: net.IPv4Mask(255, 255, 255, 0), // Unused yet
		MTU:  DefaultMTU,
	}, nil
}

// TAPDarwin is an interface for TAP device on Linux platform
type TAPDarwin struct {
	IP   net.IP           // IP
	Mask net.IPMask       // Mask
	Mac  net.HardwareAddr // Hardware Address
	Name string           // Network interface name
	Tool string           // Path to `ip`
	MTU  int              // MTU value
	file *os.File         // Interface descriptor
}

// GetName returns a name of interface
func (t *TAPDarwin) GetName() string {
	return t.Name
}

// GetHardwareAddress returns a MAC address of the interface
func (t *TAPDarwin) GetHardwareAddress() net.HardwareAddr {
	return t.Mac
}

// GetIP returns IP addres of the interface
func (t *TAPDarwin) GetIP() net.IP {
	return t.IP
}

// GetMask returns an IP mask of the interface
func (t *TAPDarwin) GetMask() net.IPMask {
	return t.Mask
}

// GetBasename returns a prefix for automatically generated interface names
func (t *TAPDarwin) GetBasename() string {
	return "tap"
}

// SetName will set interface name
func (t *TAPDarwin) SetName(name string) {
	t.Name = name
}

// SetHardwareAddress will set MAC
func (t *TAPDarwin) SetHardwareAddress(mac net.HardwareAddr) {
	t.Mac = mac
}

// SetIP will set IP
func (t *TAPDarwin) SetIP(ip net.IP) {
	t.IP = ip
}

// SetMask will set mask
func (t *TAPDarwin) SetMask(mask net.IPMask) {
	t.Mask = mask
}

// Init will initialize TAP interface creation process
func (t *TAPDarwin) Init(name string) error {
	t.Name = name
	return nil
}

// Open will open a file descriptor for a new interface
func (t *TAPDarwin) Open() error {
	var err error
	t.file, err = os.OpenFile("/dev/"+t.Name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	return nil
}
func (t *TAPDarwin) Close() error {
	if t.file != nil {
		Log(Info, "Closing network interface %s", t.GetName())
		err := t.file.Close()
		if err != nil {
			return fmt.Errorf("Failed to close network interface: %s", err)
		}
		Log(Info, "Interface closed")
		return nil
	}
	Log(Warning, "Skipping previously closed interface")
	return nil
}
func (t *TAPDarwin) Configure() error {
	setmac := exec.Command(t.Tool, t.Name, "ether", t.Mac.String())
	err := setmac.Run()
	if err != nil {
		Log(Error, "Failed to set MAC: %v", err)
	}
	// TODO: remove hardcoded mask
	linkup := exec.Command(t.Tool, t.Name, t.IP.String(), "netmask", "255.255.255.0", "up")
	err = linkup.Run()
	if err != nil {
		Log(Error, "Failed to up link: %v", err)
		return err
	}
	return nil
}

// ReadPacket will read single packet from network interface
func (t *TAPDarwin) ReadPacket() (*Packet, error) {
	buf := make([]byte, 4096)

	n, err := t.file.Read(buf)
	if err != nil {
		return nil, err
	}

	var pkt *Packet
	pkt = &Packet{Packet: buf[0:n]}
	pkt.Protocol = int(binary.BigEndian.Uint16(buf[12:14]))
	pkt.Truncated = false
	return pkt, nil
}

// WritePacket will write a single packet to interface
func (t *TAPDarwin) WritePacket(packet *Packet) error {
	n, err := t.file.Write(packet.Packet)
	if err != nil {
		return err
	}
	if n != len(packet.Packet) {
		return io.ErrShortWrite
	}
	return nil
}

// Run will start TAP processes
func (t *TAPDarwin) Run() {

}
