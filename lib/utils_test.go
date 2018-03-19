package ptp

import (
	"fmt"
	"net"
	"testing"
)

func TestGenerateMac(t *testing.T) {
	macs := make(map[string]net.HardwareAddr)

	for i := 0; i < 10000; i++ {
		smac, mac := GenerateMAC()
		if smac == "" {
			t.Errorf("Failed to generate mac")
			return
		}
		_, e := macs[smac]
		if e {
			t.Errorf("Same MAC was generated")
			return
		}
		macs[smac] = mac
	}
}

func TestFindNetworkAddresses(t *testing.T) {
	ptp := new(PeerToPeer)
	ptp.FindNetworkAddresses()
	fmt.Printf("%+v\n", ptp.LocalIPs)
}
