package main

// Control Peer and DHT Bootstrap Node

import (
	"bytes"
	"flag"
	"fmt"
	bencode "github.com/jackpal/bencode-go"
	"github.com/wayn3h0/go-uuid"
	"log"
	"net"
	"p2p/commons"
	"sort"
	"time"
)

var (
	// List of all nodes registered to current DHT bootstrap node
	// This list should always be checked if item is unique by IP and hash
	NodeList []Node

	// Ping timeout for variables
	PingTimeout time.Duration = 25
)

// Representation of a DHT Node that was connected to current DHT Bootstrap node
type Node struct {
	// Unique identifier in a form of UUID generated randomly upoc connection of a node
	ID string

	// IP Address of a node that is listening for incoming connections
	// from future network participants
	Endpoint string

	// Address that was received during connection
	ConnectionAddress string

	// Last time we pinged it.
	LastPing time.Time

	// Infohash that was associated with this node
	AssociatedHash string

	Addr *net.UDPAddr

	MissedPing int

	// When disabled - node will not be interracted.
	Disabled bool
}

// Control Peer represents a connected control peer that can be used by
// normal peers to forward their traffic
type ControlPeer struct {
	Addr *net.UDPAddr
}

// Infohash is a 20-bytes string and associated IP Address
// There must be multiple infohashes, but each infohash should
// have unique IP address, because we don't want to response
// multiple times with same IP for same infohash
type Infohash struct {
	// 20 bytes infohash string
	Hash string

	// Address associated with this hash
	NodeAddr string
}

// Router class
type DHTRouter struct {
	// Number of nodes participating in DHT
	NodesNumber int

	// Port which DHT router listens
	Port int

	// List of infohashes
	Hashes []Infohash

	Connection *net.UDPConn

	ControlPeers []ControlPeer
}

// Method ValidateConnection() tries to establish connection with control
// peer to check is it's accessible from outside.
// Return true if CP is able to received connection, false otherwise
func (cp *ControlPeer) ValidateConnection() bool {
	conn, err := net.DialUDP("udp", nil, cp.Addr)
	if err != nil {
		return false
	}
	// TODO: Send something to CP
	err = conn.Close()
	if err != nil {
		log.Printf("[ERROR] Failed to close connection with control peer: %v", err)
	}
	return true
}

// Generate UUID, assigns it to a node and returns UUID as a string
// This methods always checks if generated ID is unique
func (node *Node) GenerateID(hashes []Infohash) string {
	var err error
	var id uuid.UUID
	id, err = uuid.NewTimeBased()
	if err != nil {
		log.Panic("[ERROR] Failed to generate UUID: %v", err)
		node.ID = ""
	} else {
		// Check if UUID is unique here
		var unique bool
		unique = true
		for _, hash := range hashes {
			if hash.Hash == id.String() {
				unique = false
			}
		}
		if unique {
			node.ID = id.String()
		} else {
			node.ID = node.GenerateID(hashes)
		}
	}
	return node.ID
}

// Functions returns true if timeout period has passed since last ping
func (node *Node) isPingRequired(n *Node) bool {
	return false
}

// Currently unused
func handleConnection(c *net.Conn) int {
	return 1
}

// SetupServers prepares a DHT router listening socket that DHT clients
// will send UDP packets to
func (dht *DHTRouter) SetupServer() *net.UDPConn {
	log.Printf("[INFO] Setting UDP server at %d port", dht.Port)
	udp, err := net.ListenUDP("udp4", &net.UDPAddr{Port: dht.Port})
	if err != nil {
		log.Printf("[ERROR] Failed to start UDP Listener: %v", err)
		return nil
	}
	return udp
}

// IsNewPeer returns true if connected peer was not connected yes, false otherwise
func (dht *DHTRouter) IsNewPeer(addr string) bool {
	// TODO: Rewrite with use of ranges
	for _, node := range NodeList {
		if node.ConnectionAddress == addr {
			return false
		}
	}
	return true
}

// Extracts DHTRequest from received packet
// This method tries to unmarshal bencode into DHTRequest structure
func (dht *DHTRouter) Extract(b []byte) (request commons.DHTRequest, err error) {
	defer func() {
		if x := recover(); x != nil {
			log.Printf("[ERROR] Bencode Unmarshal failed %q, %v", string(b), x)
		}
	}()
	if err2 := bencode.Unmarshal(bytes.NewBuffer(b), &request); err2 == nil {
		err = nil
		return
	} else {
		log.Printf("[DEBUG] Received from peer: %v %q", request, err2)
		return request, err2
	}
}

// Returns a bencoded representation of a DHTResponse
func (dht *DHTRouter) Compose(command, id, dest string) string {
	var resp commons.DHTResponse
	// Command is mandatory
	resp.Command = command
	// Defaults
	resp.Id = "0"
	resp.Dest = "0"
	if id != "" {
		resp.Id = id
	}
	if dest != "" {
		resp.Dest = dest
	}
	return dht.EncodeResponse(resp)
}

// EncodeResponse takes DHTResponse structure and turns it into bencode by
// Marshaling
func (dht *DHTRouter) EncodeResponse(resp commons.DHTResponse) string {
	if resp.Command == "" {
		return ""
	}
	var b bytes.Buffer
	if err := bencode.Marshal(&b, resp); err != nil {
		log.Printf("[ERROR] Failed to Marshal bencode %v", err)
		return ""
	}
	return b.String()
}

// ResponseConn method generates a response to a "conn" network message received as a first packet
// from a newly connected node. Response writes an ID of the node
func (dht *DHTRouter) ResponseConn(req commons.DHTRequest, addr string, n Node) commons.DHTResponse {
	// First we want to update Endpoint for this node
	// Let's resolve new address from original IP and by port received from client
	a1, _ := net.ResolveUDPAddr("udp", addr)
	a, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%s", a1.IP.String(), req.Port))
	if err != nil {
		log.Printf("[DHT-ERROR] Failed to resolve UDP Address: %v", err)
	}
	for i, node := range NodeList {
		if node.ConnectionAddress == addr {
			NodeList[i].Endpoint = a.String()
		}
	}
	var resp commons.DHTResponse
	resp.Command = req.Command
	resp.Id = n.ID
	resp.Dest = "0"
	return resp
}

func (dht *DHTRouter) RegisterHash(addr string, hash string) {
	for i, node := range NodeList {
		if node.ConnectionAddress == addr {
			NodeList[i].AssociatedHash = hash
			log.Printf("[DEBUG] Registering hash '%s' for %s", hash, addr)
		}
	}
}

// ResponseFind method generates a response to a "find" network message which sent by DHT client
// when they want to build a p2p network based on infohash string.
// This method goes over list of hashes and collects information about all nodes with the
// same hash separated by comma
func (dht *DHTRouter) ResponseFind(req commons.DHTRequest, addr string) commons.DHTResponse {
	for _, n := range NodeList {
		log.Printf("Node: %s, Hash: %s", n.ConnectionAddress, n.AssociatedHash)
	}

	var foundDest string
	var hashExists bool = false
	for _, node := range NodeList {
		if node.AssociatedHash == req.Hash {
			if node.ConnectionAddress == addr {
				hashExists = true
				// Skip if we are the node who requested hash
				continue
			}
			log.Printf("[DEBUG] Found match in hash '%s' with peer %s", req.Hash, node.AssociatedHash)
			foundDest += node.Endpoint + ","
		}
	}
	if !hashExists {
		// Hash was not found for current node. Add it
		dht.RegisterHash(addr, req.Hash)
	}
	var resp commons.DHTResponse
	resp.Command = req.Command
	resp.Id = "0"
	resp.Dest = foundDest
	return resp
}

// ResponsePing responses to a received "ping" message
func (dht *DHTRouter) ResponsePing(req commons.DHTRequest, addr string) commons.DHTResponse {
	var resp commons.DHTResponse
	resp.Command = req.Command
	resp.Id = "0"
	resp.Dest = "0"
	return resp
}

// ResponseRegCP will check newly connected CP if it was not connected before. Also,
// this method will call a function that will try to connect to CP to see if it's
// accessible from outside it's network and not blocked by NAT, so normal peers
// can connect to it
func (dht *DHTRouter) ResponseRegCP(req commons.DHTRequest, addr string) commons.DHTResponse {
	var resp commons.DHTResponse
	resp.Command = req.Command
	resp.Id = "0"
	resp.Dest = "0"
	laddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		log.Printf("[ERROR] Failed to extract CP address: %v", err)
		resp.Command = ""
	} else {
		var isNew bool = true
		for _, cp := range dht.ControlPeers {
			if cp.Addr.IP.String() == laddr.IP.String() {
				isNew = false
			}
		}
		if !isNew {
			// At this point we will send an empty response, so CP will try
			// to reconnect later, when it's previous instance will be wiped
			// from list after PING timeout
			log.Printf("[ERROR] Connected control peer is already in list")
			resp.Command = ""
		} else {
			var newCP ControlPeer
			newCP.Addr = laddr
			if !newCP.ValidateConnection() {
				log.Printf("[ERROR] Failed to connect to Control Peer. Ignoring")
				resp.Command = ""
			} else {
				// TODO: Consider assigning ID to Control Peers, but currently we
				// don't need such functionality
				dht.ControlPeers = append(dht.ControlPeers, newCP)
			}
		}
	}
	return resp
}

// ResponseCP responses to a CP request
func (dht *DHTRouter) ResponseCP(req commons.DHTRequest, addr string) commons.DHTResponse {
	var resp commons.DHTResponse
	resp.Command = req.Command
	resp.Id = "0"
	resp.Dest = "0"
	for _, cp := range dht.ControlPeers {
		if cp.ValidateConnection() {
			resp.Dest = cp.Addr.String()
		}
	}
	return resp
}

// Send method send a packet to a connected client over network to a specific UDP address
func (dht *DHTRouter) Send(conn *net.UDPConn, addr *net.UDPAddr, msg string) {
	if msg != "" {
		_, err := conn.WriteToUDP([]byte(msg), addr)
		if err != nil {
			log.Printf("[ERROR] Failed to write to UDP: %v", err)
		}
	}
}

// This method listens to a UDP connections for incoming packets and
// sends generated responses back to DHT nodes
func (dht *DHTRouter) Listen(conn *net.UDPConn) {
	var buf [512]byte
	_, addr, err := conn.ReadFromUDP(buf[0:])
	if err != nil {
		log.Printf("[ERROR] Failed to read from UDP socket: %v", err)
		return
	}
	var n Node
	if dht.IsNewPeer(addr.String()) {
		log.Printf("[INFO] New Peer connected: %s. Registering", addr)
		n.ID = n.GenerateID(dht.Hashes)
		n.Endpoint = ""
		n.ConnectionAddress = addr.String()
		n.Addr = addr
		n.AssociatedHash = ""
		NodeList = append(NodeList, n)
	}
	log.Printf("[DEBUG] %s: %s", addr, string(buf[:512]))

	// Try to bencode
	req, err := dht.Extract(buf[:512])
	var resp commons.DHTResponse
	switch req.Command {
	case commons.CMD_CONN:
		// Connection handshake
		resp = dht.ResponseConn(req, addr.String(), n)
	case commons.CMD_FIND:
		// Find by infohash request
		resp = dht.ResponseFind(req, addr.String())
	case commons.CMD_PING:
		for i, node := range NodeList {
			if node.Addr.String() == addr.String() {
				NodeList[i].MissedPing = 0
			}
		}
		resp.Command = ""
	case commons.CMD_REGCP:
		// Register new control peer
		resp = dht.ResponseRegCP(req, addr.String())
	case commons.CMD_CP:
		// Find control peer
		resp = dht.ResponseCP(req, addr.String())
	case commons.CMD_BADCP:
		// Given Control Peer cannot be communicated
		// TODO: Move this to a separate method
		for i, cp := range dht.ControlPeers {
			if cp.Addr.String() == req.Hash {
				if !cp.ValidateConnection() {
					// Remove bad control peer
					dht.ControlPeers = append(dht.ControlPeers[:i], dht.ControlPeers[i+1:]...)
					break
				}
			}
		}
		// TODO: Exclude this Control peer from list for this particular peer
		resp = dht.ResponseCP(req, addr.String())
	default:
		log.Printf("[ERROR] Unknown command received: %s", req.Command)
		resp.Command = ""
	}

	if resp.Command != "" {
		dht.Send(conn, addr, dht.EncodeResponse(resp))
	}
}

// Ping method is running as a goroutine. Ininity loop will
// ping every client after a timeout.
func (dht *DHTRouter) Ping(conn *net.UDPConn) {
	req := new(commons.DHTRequest)
	req.Command = "ping"
	var removeKeys []int
	for {
		for _, i := range removeKeys {
			NodeList = append(NodeList[:i], NodeList[i+1:]...)
			log.Printf("[NOTICE] %s timeout reached. Disconnecting", NodeList[i].ConnectionAddress)
		}
		removeKeys = removeKeys[:0]
		time.Sleep(PingTimeout * time.Second)
		for i, node := range NodeList {
			NodeList[i].MissedPing = NodeList[i].MissedPing + 1
			resp := dht.ResponsePing(*req, node.ConnectionAddress)
			dht.Send(conn, node.Addr, dht.EncodeResponse(resp))
			if NodeList[i].MissedPing >= 4 {
				removeKeys = append(removeKeys, i)
				NodeList[i].Disabled = true
			}
		}
		sort.Sort(sort.Reverse(sort.IntSlice(removeKeys)))
	}
}

func main() {
	var argDht int
	flag.IntVar(&argDht, "dht", -1, "Port that DHT Bootstrap will listening to")
	flag.Parse()
	log.Printf("[INFO] Initialization complete")
	log.Printf("[INFO] Starting bootstrap node")
	if argDht > 0 {
		var dht DHTRouter
		dht.Port = argDht
		dht.Connection = dht.SetupServer()

		go dht.Ping(dht.Connection)

		for {
			dht.Listen(dht.Connection)
		}
	} else {
		// Act as a normal (proxy) control peer
		var proxy Proxy
		proxy.Initialize()
		for {
		}
	}
}
