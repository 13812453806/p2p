package main

// Connect to a DHT
// Register with DHT
// Wait for incoming connections
// Validate incoming connections with DHT

import (
	"fmt"
	"net"
	"p2p/dht"
	"p2p/udpcs"
	"time"
)

type Proxy struct {
	DHTClient *dht.DHTClient
	Tunnels   map[int]Tunnel
	UDPServer *udpcs.UDPClient
}

// Tunnel established between two peers. Tunnels doesn't
// provide two-way connectivity.
type Tunnel struct {
	Src *net.UDPAddr
	Dst *net.UDPAddr
}

func (p *Proxy) Initialize() {
	p.UDPServer = new(udpcs.UDPClient)
	p.UDPServer.Init("", 0)
	p.DHTClient = new(dht.DHTClient)
	config := p.DHTClient.DHTClientConfig()
	config.NetworkHash = p.GenerateHash()
	config.P2PPort = p.UDPServer.GetPort()
	var ips []net.IP
	p.DHTClient.Initialize(config, ips)
}

func (p *Proxy) GenerateHash() string {
	var infohash string
	t := time.Now()
	infohash = "cp" + fmt.Sprintf("%d%d%d", t.Year(), t.Month(), t.Day())
	return infohash
}
