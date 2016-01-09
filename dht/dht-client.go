package dht

import (
	"bytes"
	"fmt"
	bencode "github.com/jackpal/bencode-go"
	"log"
	"net"
	"p2p/commons"
	"p2p/go-stun/stun"
	"strings"
)

type DHTClient struct {
	Routers       string
	FailedRouters []string
	Connection    []*net.UDPConn
	NetworkHash   string
	NetworkPeers  []string
	P2PPort       int
	LastCatch     []string
	ID            string
}

func (dht *DHTClient) DHTClientConfig() *DHTClient {
	return &DHTClient{
		//Routers: "localhost:6881",
		Routers: "dht1.subut.ai:6881",
		//Routers: "172.16.192.5:6881",
		//Routers:     "dht1.subut.ai:6881,dht2.subut.ai:6881,dht3.subut.ai:6881,dht4.subut.ai:6881,dht5.subut.ai:6881",
		NetworkHash: "",
	}
}

// AddConnection adds new UDP Connection reference onto list of DHT node connections
func (dht *DHTClient) AddConnection(connections []*net.UDPConn, conn *net.UDPConn) []*net.UDPConn {
	n := len(connections)
	if n == cap(connections) {
		newSlice := make([]*net.UDPConn, len(connections), 2*len(connections)+1)
		copy(newSlice, connections)
		connections = newSlice
	}
	connections = connections[0 : n+1]
	connections[n] = conn
	return connections
}

// ConnectAndHandshake sends an initial packet to a DHT bootstrap node
func (dht *DHTClient) ConnectAndHandshake(router string, ips []net.IP) (*net.UDPConn, error) {
	log.Printf("[DHT-INFO] Connecting to a router %s", router)
	addr, err := net.ResolveUDPAddr("udp", router)
	if err != nil {
		log.Printf("[DHT-ERROR]: Failed to resolve router address: %v", err)
		return nil, err
	}

	conn, err := net.DialUDP("udp4", nil, addr)
	if err != nil {
		log.Printf("[DHT-ERROR]: Failed to establish connection: %v", err)
		return nil, err
	}

	log.Printf("[DHT-INFO] Ready to bootstrap with %s [%s]", router, conn.RemoteAddr().String())

	// Handshake
	var req commons.DHTRequest
	req.Id = "0"
	req.Hash = "0"
	req.Command = "conn"
	// TODO: rename Port to something more clear
	req.Port = fmt.Sprintf("%d", dht.P2PPort)
	for _, ip := range ips {
		req.Port = req.Port + "|" + ip.String()
	}
	var b bytes.Buffer
	if err := bencode.Marshal(&b, req); err != nil {
		log.Printf("[DHT-ERROR] Failed to Marshal bencode %v", err)
		conn.Close()
		return nil, err
	}
	// TODO: Optimize types here
	msg := b.String()
	_, err = conn.Write([]byte(msg))
	if err != nil {
		log.Printf("[DHT-ERROR] Failed to send packet: %v", err)
		conn.Close()
		return nil, err
	}

	return conn, nil
}

// Extracts DHTRequest from received packet
func (dht *DHTClient) Extract(b []byte) (response commons.DHTResponse, err error) {
	defer func() {
		if x := recover(); x != nil {
			log.Printf("[DHT-ERROR] Bencode Unmarshal failed %q, %v", string(b), x)
		}
	}()
	if e2 := bencode.Unmarshal(bytes.NewBuffer(b), &response); e2 == nil {
		err = nil
		return
	} else {
		log.Printf("[DHT-DEBUG] Received from peer: %v %q", response, e2)
		return response, e2
	}
}

// Returns a bencoded representation of a DHTRequest
func (dht *DHTClient) Compose(command, id, hash string) string {
	var req commons.DHTRequest
	// Command is mandatory
	req.Command = command
	// Defaults
	req.Id = "0"
	req.Hash = "0"
	if id != "" {
		req.Id = id
	}
	if hash != "" {
		req.Hash = hash
	}
	return dht.EncodeRequest(req)
}

func (dht *DHTClient) EncodeRequest(req commons.DHTRequest) string {
	if req.Command == "" {
		return ""
	}
	var b bytes.Buffer
	if err := bencode.Marshal(&b, req); err != nil {
		log.Printf("[ERROR] Failed to Marshal bencode %v", err)
		return ""
	}
	return b.String()
}

// After receiving a list of peers from DHT we will parse the list
// and add every new peer into list of peers
func (dht *DHTClient) UpdateLastCatch(catch string) {
	peers := strings.Split(catch, ",")
	for _, p := range peers {
		if p == "" {
			continue
		}
		var found bool = false
		for _, catchedPeer := range dht.LastCatch {
			if p == catchedPeer {
				found = true
			}
		}
		if !found {
			dht.LastCatch = append(dht.LastCatch, p)
		}
	}
}

// This function sends a request to DHT bootstrap node with ID of
// target node we want to connect to
func (dht *DHTClient) RequestPeersIPs(id string) {
	msg := dht.Compose(commons.CMD_NODE, id, "")
	for _, conn := range dht.Connection {
		_, err := conn.Write([]byte(msg))
		if err != nil {
			log.Printf("[DHT-ERROR] Failed to send 'node' request to %s: %v", conn.RemoteAddr().String(), err)
		}
	}
}

// UpdatePeers sends "find" request to a DHT Bootstrap node, so it can respond
// with a list of peers that we can connect to
// This method should be called periodically in case any new peers was discovered
func (dht *DHTClient) UpdatePeers() {
	msg := dht.Compose(commons.CMD_FIND, "", dht.NetworkHash)
	for _, conn := range dht.Connection {
		log.Printf("[DHT-DEBUG] Updating peer %s", conn.RemoteAddr().String())
		_, err := conn.Write([]byte(msg))
		if err != nil {
			log.Printf("[DHT-ERROR] Failed to send 'find' request to %s: %v", conn.RemoteAddr().String(), err)
		}
	}
}

// Listens for packets received from DHT bootstrap node
// Every packet is unmarshaled and turned into Request structure
// which we should analyze and respond
func (dht *DHTClient) ListenDHT(conn *net.UDPConn) string {
	log.Printf("[DHT-INFO] Bootstraping via %s", conn.RemoteAddr().String())
	for {
		var buf [512]byte
		//_, addr, err := conn.ReadFromUDP(buf[0:])
		_, _, err := conn.ReadFromUDP(buf[0:])
		if err != nil {
			log.Printf("[DHT-ERROR] Failed to read from DHT bootstrap node: %v", err)
		} else {
			data, err := dht.Extract(buf[:512])
			if err != nil {
				log.Printf("[DHT-ERROR] Failed to extract a message: %v", err)
			} else {
				if data.Command == commons.CMD_CONN {
					dht.ID = data.Id
					// Send a hash within FIND command
					// Afterwards application should wait for response from DHT
					// with list of clients. This may not happen if this client is the
					// first connected node.
					msg := dht.Compose(commons.CMD_FIND, "", dht.NetworkHash)
					_, err = conn.Write([]byte(msg))
					if err != nil {
						log.Printf("[DHT-ERROR] Failed to send FIND packet: %v", err)
					} else {
						log.Printf("[DHT-INFO] Received connection confirmation from tracker %s", conn.RemoteAddr().String())
					}
				} else if data.Command == commons.CMD_PING {
					msg := dht.Compose(commons.CMD_PING, "", "")
					_, err = conn.Write([]byte(msg))
					if err != nil {
						log.Printf("[DHT-ERROR] Failed to send PING packet: %v", err)
					}
				} else if data.Command == commons.CMD_FIND {
					// This means we've received a list of nodes we can connect to
					if data.Dest != "" {
						//log.Printf("[DHT-INFO] Found peers from %s: %s", conn.RemoteAddr().String(), data.Dest)
						dht.UpdateLastCatch(data.Dest)
					}
				} else if data.Command == commons.CMD_REGCP {
					// We've received a registration confirmation message from DHT bootstrap node
				} else if data.Command == commons.CMD_NODE {
					// We've received an IPs associated with target node
				}
			}
		}
	}
}

// This method initializes DHT by splitting list of routers and connect to each one
func (dht *DHTClient) Initialize(config *DHTClient, ips []net.IP) *DHTClient {
	dht = config
	routers := strings.Split(dht.Routers, ",")
	dht.FailedRouters = make([]string, len(routers))
	for _, router := range routers {
		conn, err := dht.ConnectAndHandshake(router, ips)
		if err != nil || conn == nil {
			dht.FailedRouters[0] = router
		} else {
			dht.Connection = append(dht.Connection, conn)
			go dht.ListenDHT(conn)
		}
	}
	return dht
}

var nat_type_str = [...]string{"NAT_ERROR", "NAT_UNKNOWN", "NAT_NONE", "NAT_BLOCKED",
	"NAT_FULL", "NAT_SYMETRIC", "NAT_RESTRICTED", "NAT_PORT_RESTRICTED", "NAT_SYMETRIC_UDP_FIREWALL"}

func DetectIP() string {
	stun_client := stun.NewClient()
	stun_client.LocalAddr = ""
	stun_client.LocalPort = 15000
	stun_client.SetSoftwareName("subutai")
	stun_client.SetServerHost("stun.iptel.org", 3478)
	_, host, err := stun_client.Discover()
	if err != nil {
		log.Printf("Stun discover error %v\n", err)
		return ""
	}
	//fmt.Printf("%s\n", nat_type_str[nat_type])
	if host != nil {
		return host.IP()
		//fmt.Printf("%s:%d\n", host.IP(), host.Port())
	}
	return ""
}
