package ptp

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"
)

// StateHandlerCallback is a peer method callback executed by peer state
type StateHandlerCallback func(ptpc *PeerToPeer) error

type PeerEndpoint struct {
	Addr        *net.UDPAddr
	LastContact time.Time
}

// NetworkPeer represents a peer
type NetworkPeer struct {
	ID                 string                             // ID of a peer
	ProxyID            uint16                             // ID of the proxy
	Forwarder          *net.UDPAddr                       // Forwarder address
	PeerAddr           *net.UDPAddr                       // Address of peer
	Endpoint           *net.UDPAddr                       // Endpoint address of a peer. TODO: Make this net.UDPAddr
	KnownIPs           []*net.UDPAddr                     // List of IP addresses that accepts connection on peer
	Proxies            []*net.UDPAddr                     // List of proxies of this peer
	PeerLocalIP        net.IP                             // IP of peers interface. TODO: Rename to IP
	PeerHW             net.HardwareAddr                   // Hardware address of peer interface. TODO: Rename to Mac
	State              PeerState                          // State of a peer on our end
	RemoteState        PeerState                          // State of remote peer
	LastContact        time.Time                          // Last ping with this peer
	PingCount          uint8                              // Number of pings messages sent without response
	LastError          string                             // Test of last error occured during state execution
	ForceProxy         bool                               // Whether we are forced to use proxy or not
	TestPacketReceived bool                               // Whether or not test packet were received
	ConnectionAttempts uint8                              // How many times we tried to connect
	stateHandlers      map[PeerState]StateHandlerCallback // List of callbacks for different peer states
	IsUsingTURN        bool                               // Whether or not we are currently connected via TURN
	Running            bool                               // Whether peer is running or not
	Endpoints          []PeerEndpoint                     // List of active endpoints
}

func (np *NetworkPeer) reportState(ptpc *PeerToPeer) {
	stateStr := strconv.Itoa(int(np.State))
	if stateStr == "" {
		return
	}
	ptpc.Dht.sendState(np.ID, stateStr)
}

// SetState modify local state of peer
func (np *NetworkPeer) SetState(state PeerState, ptpc *PeerToPeer) {
	np.State = state
	np.reportState(ptpc)
}

// NetworkPeerState represents a state for remote peers
type NetworkPeerState struct {
	ID    string // Peer's ID
	State string // State of peer
}

// Run is main loop for a peer
func (np *NetworkPeer) Run(ptpc *PeerToPeer) {
	np.Running = true
	np.ConnectionAttempts = 0
	for {
		if np.State == PeerStateStop {
			Log(Info, "Stopping peer %s", np.ID)
			break
		}
		if ptpc.Dht.ID == "" {
			time.Sleep(time.Millisecond * 500)
			continue
		}
		np.stateHandlers = make(map[PeerState]StateHandlerCallback)
		np.stateHandlers[PeerStateInit] = np.stateInit
		np.stateHandlers[PeerStateRequestedIP] = np.stateRequestedIP
		np.stateHandlers[PeerStateConnecting] = np.stateConnecting
		np.stateHandlers[PeerStateConnectingDirectlyWait] = np.stateConnectingDirectlyWait
		np.stateHandlers[PeerStateConnectingDirectly] = np.stateConnectingDirectly
		np.stateHandlers[PeerStateConnectingInternetWait] = np.stateConnectingInternetWait
		np.stateHandlers[PeerStateConnectingInternet] = np.stateConnectingInternet
		np.stateHandlers[PeerStateConnected] = np.stateConnected
		np.stateHandlers[PeerStateHandshaking] = np.stateHandshaking
		np.stateHandlers[PeerStateWaitingForwarder] = np.stateWaitingForwarder
		np.stateHandlers[PeerStateHandshakingForwarder] = np.stateHandshakingForwarder
		np.stateHandlers[PeerStateHandshakingFailed] = np.stateHandshakingFailed
		np.stateHandlers[PeerStateDisconnect] = np.stateDisconnect
		np.stateHandlers[PeerStateStop] = np.stateStop

		np.stateHandlers[PeerStateRequestingProxy] = np.stateRequestingProxy
		np.stateHandlers[PeerStateWaitingForProxy] = np.stateWaitingForProxy
		np.stateHandlers[PeerStateWaitingToConnect] = np.stateWaitingToConnect
		np.stateHandlers[PeerStateRouting] = np.stateRouting

		callback, exists := np.stateHandlers[np.State]
		if !exists {
			Log(Error, "Peer %s is in unknown state: %d", np.ID, int(np.State))
			time.Sleep(1 * time.Second)
			continue
		}
		err := callback(ptpc)
		if err != nil {
			Log(Warning, "Peer %s: %v", np.ID, err)
		}
		time.Sleep(time.Millisecond * 500)
	}
	Log(Info, "Peer %s has been stopped", np.ID)
}

// State: Peer Initialization
// Initialize variables
// Automatically switch to PeerStateRequestedIP or PeerStateDisconnect if
// too many connection attempts were failed
func (np *NetworkPeer) stateInit(ptpc *PeerToPeer) error {
	// Send request about IPs of a peer
	Log(Info, "Initializing new peer: %s", np.ID)
	ptpc.Dht.sendNode(np.ID)
	np.KnownIPs = np.KnownIPs[:0]
	// Do some variables cleanup
	np.Endpoint = nil
	np.PeerAddr = nil
	np.PeerHW = nil
	np.PeerLocalIP = nil
	np.TestPacketReceived = false
	np.IsUsingTURN = false
	np.SetState(PeerStateRequestedIP, ptpc)
	np.ConnectionAttempts++
	if np.ConnectionAttempts > 5 {
		np.SetState(PeerStateDisconnect, ptpc)
		return fmt.Errorf("Too many unsuccessfull connection attempts")
	}
	return nil
}

// stateRequestedIP will wait for a DHT client to receive an IPs for this peer
// State: Requested peer IP
// Send `node` request and wait for known IP addresses of the peer from DHT
// If peer doesn't receive endpoints in the timely manner method will switch to
// PeerStateDisconnect. On success it will switch to PeerStateConnecting
func (np *NetworkPeer) stateRequestedIP(ptpc *PeerToPeer) error {
	Log(Info, "Waiting network addresses for peer: %s", np.ID)
	requestSentAt := time.Now()
	updateInterval := time.Duration(time.Millisecond * 1000)
	attempts := 0
	for {
		if time.Since(requestSentAt) > updateInterval {
			Log(Warning, "Didn't got network addresses for peer. Requesting again")
			requestSentAt = time.Now()
			err := ptpc.Dht.sendNode(np.ID)
			if err != nil {
				np.SetState(PeerStateDisconnect, ptpc)
				return fmt.Errorf("Failed to request IPs: %s", err)
			}
			attempts++
		}
		if attempts > 5 {
			np.SetState(PeerStateDisconnect, ptpc)
			break
		}
		if len(np.KnownIPs) > 0 {
			np.SetState(PeerStateRequestingProxy, ptpc)
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// State: Connecting
// Entry point for connection establishment process
// func (np *NetworkPeer) stateConnecting(ptpc *PeerToPeer) error {
// 	np.SetState(PeerStateConnectingDirectlyWait, ptpc)
// 	return nil
// }

// State: Waiting for direct connection with peer
// This method will wait for specific period of time for other peer to join the same
// or required state.
// Once other peer reached reqiuired state peer will switch to PeerStateConnectingDirectly
// If timeout has passed it will switch to same state to force direct connection
func (np *NetworkPeer) stateConnectingDirectlyWait(ptpc *PeerToPeer) error {
	// We don't want to do this for more than 5 minutes
	Log(Info, "Waiting for other peer to start connecting directly")
	started := time.Now()
	for {
		if np.State != PeerStateConnectingDirectlyWait {
			return nil
		}
		if np.RemoteState == PeerStateConnectingDirectlyWait || np.RemoteState == PeerStateConnectingDirectly {
			Log(Info, "Second peer has joined required state")
			np.SetState(PeerStateConnectingDirectly, ptpc)
			break
		}
		time.Sleep(100 * time.Millisecond)
		passed := time.Since(started)
		if passed > time.Duration(1*time.Minute) {
			np.SetState(PeerStateConnectingDirectly, ptpc)
			return fmt.Errorf("Wait for direct connection failed: Peer doesn't responded in a timely manner")
		}
	}
	return nil
}

// State: Establishing direct connection over LAN
// This method will switch peer to PeerStateWaitingForwarder if forced
// proxy mode is enabled.
// Method will attempt to establish connection with peer over LAN by
// taking private IP addresses for a list of known endpoints.
// If LAN connection is established this method will switch to PeerStateHandshaking
// Otherwise it will switch to PeerStateConnectingInternetWait
func (np *NetworkPeer) stateConnectingDirectly(ptpc *PeerToPeer) error {
	np.IsUsingTURN = false
	Log(Info, "Trying direct connection with peer: %s", np.ID)
	if len(np.KnownIPs) == 0 {
		np.SetState(PeerStateInit, ptpc)
		np.LastError = fmt.Sprintf("Didn't received any IP addresses")
		return errors.New("Joined connection state without knowing any IPs")
	}
	// If forward mode was activated - skip direct connection attempts
	if ptpc.ForwardMode || np.ForceProxy {
		Log(Info, "Forcing switch to proxy usage")
		np.SetPeerAddr()
		np.SetState(PeerStateWaitingForwarder, ptpc)
		return nil
	}
	// Try to connect locally
	isLocal := np.ProbeLocalConnection(ptpc)

	if isLocal {
		np.PeerAddr = np.Endpoint
		Log(Info, "Connected with %s over LAN", np.ID)
		np.SetState(PeerStateHandshaking, ptpc)
		return nil
	}
	Log(Info, "Can't connect with %s over LAN", np.ID)

	np.SetState(PeerStateConnectingInternetWait, ptpc)
	return nil
}

// State: Waiting for internet connection with peer.
// This method will wait for other peer to join the same state to start
// establishing internet connection over internet. This is required
// for UDP hole punching process to start connection process at the same time
// When peer joins required state this method will switch to PeerStateConnectingInternet
// Otherwise it will switch to the same state to force internet connection process
func (np *NetworkPeer) stateConnectingInternetWait(ptpc *PeerToPeer) error {
	// We don't want to do this for more than 5 minutes
	Log(Info, "Waiting for other peer to start connecting over Internet")
	started := time.Now()
	for {
		if np.State != PeerStateConnectingInternetWait {
			return nil
		}
		if np.RemoteState == PeerStateConnectingInternetWait || np.RemoteState == PeerStateConnectingInternet {
			newState := "Waiting for internet connection"
			if np.RemoteState == PeerStateConnectingInternet {
				newState = "Connecting over internet"
			}
			Log(Info, "Second peer joined required state: %s", newState)
			np.SetState(PeerStateConnectingInternet, ptpc)
			break
		}
		time.Sleep(100 * time.Millisecond)
		passed := time.Since(started)
		if passed > time.Duration(1*time.Minute) {
			np.SetState(PeerStateConnectingInternet, ptpc)
			return fmt.Errorf("Wait for internet connection failed: Peer doesn't responded in a timely manner")
		}
	}
	return nil
}

// State: Establish connection with peer over Internet
// This method will start UDP hole punching process to all public endpoints
// of the peer.
// If connection is established method will switch to PeerStateHandshaking
// Otherwise it will switch to PeerStateWaitingForwarder
func (np *NetworkPeer) stateConnectingInternet(ptpc *PeerToPeer) error {
	np.IsUsingTURN = false
	for _, addr := range np.KnownIPs {
		ip := addr.IP
		isPrivate, err := isPrivateIP(ip)
		if err != nil {
			Log(Error, "%s", err)
			continue
		}
		if isPrivate {
			Log(Debug, "Skipping private IP %s", ip.String())
			continue
		}
		np.Endpoint = addr
		Log(Info, "Attempting to connect with %s over Internet [%s]", np.ID, np.Endpoint.String())
		success := np.holePunch(addr, ptpc)
		if success {
			np.PeerAddr = np.Endpoint
			Log(Info, "Connected with %s over Internet", np.ID)
			np.SetState(PeerStateHandshaking, ptpc)
			return nil
		}
	}
	np.SetPeerAddr()
	np.SetState(PeerStateWaitingForwarder, ptpc)
	return fmt.Errorf("Internet connection with %s failed", np.ID)
}

// stateHandshaking is executed when we're waiting for handshake to complete
func (np *NetworkPeer) stateHandshaking(ptpc *PeerToPeer) error {
	Log(Info, "Sending handshake to %s", np.ID)
	handshakeSentAt := time.Now()
	for np.State == PeerStateHandshaking {
		passed := time.Since(handshakeSentAt)
		if passed > time.Duration(time.Second*14) {
			np.SetState(PeerStateHandshakingFailed, ptpc)
			return fmt.Errorf("Failed to handshake with peer %s", np.ID)
		}
		np.sendHandshake(ptpc, false)
		time.Sleep(time.Millisecond * 500)
	}
	return nil
}

// stateHandshakingFailed is executed when we've failed to handshake a peer
func (np *NetworkPeer) stateHandshakingFailed(ptpc *PeerToPeer) error {
	if np.Forwarder != nil {
		np.LastError = "Failed to handshake with this peer over forwarder"
		Log(Error, "Failed to handshake with %s via proxy %s", np.ID, np.Forwarder.String())
		np.Forwarder = nil
		np.SetState(PeerStateDisconnect, ptpc)
	} else {
		np.LastError = "Failed to handshake with this peer"
		Log(Error, "Failed to handshake directly. Switching to proxy")
	}
	np.SetState(PeerStateWaitingForwarder, ptpc)
	return nil
}

// stateWaitingForwarder will wait for a proxy address
// Proxy was requested from DHT. This state waits for proxy
// address
func (np *NetworkPeer) stateWaitingForwarder(ptpc *PeerToPeer) error {
	Log(Info, "Looking in a list of cached proxies")

	for _, fwd := range ptpc.Dht.Forwarders {
		if fwd.DestinationID == np.ID {
			np.Forwarder = fwd.Addr
			np.Endpoint = fwd.Addr
			np.SetState(PeerStateHandshakingForwarder, ptpc)
			Log(Info, "Found cached forwarder")
			return nil
		}
	}

	Log(Info, "Requesting proxy for %s", np.ID)
	np.RequestForwarder(ptpc)
	waitStart := time.Now()
	for len(np.Proxies) == 0 {
		time.Sleep(time.Millisecond * 100)
		passed := time.Since(waitStart)
		if passed > WaitProxyTimeout {
			np.SetState(PeerStateDisconnect, ptpc)
			np.LastError = "No forwarders received"
			return fmt.Errorf("No proxy were received for %s", np.ID)
		}
	}
	np.SetState(PeerStateHandshakingForwarder, ptpc)
	return nil
}

// stateHandshakingForwarder waits for handshake with a proxy to be completed
func (np *NetworkPeer) stateHandshakingForwarder(ptpc *PeerToPeer) error {
	np.IsUsingTURN = true
	for _, proxy := range np.Proxies {
		np.Endpoint = proxy
		Log(Info, "Sending handshake to %s over forwarder %s", np.ID, np.Endpoint.String())
		handshakeSentAt := time.Now()
		for np.State == PeerStateHandshakingForwarder {
			passed := time.Since(handshakeSentAt)
			if passed > time.Duration(time.Second*10) {
				// Stop attempts to connect over specified forwarder and switch to next
				break
			}
			np.sendHandshake(ptpc, true)
			time.Sleep(time.Millisecond * 500)
		}
		if np.State != PeerStateHandshakingForwarder {
			return nil
		}
	}
	np.SetState(PeerStateHandshakingFailed, ptpc)
	return fmt.Errorf("Failed to handshake with peer %s over TURN", np.ID)
}

// stateConnected is executed when connection was established and peer is operating normally
func (np *NetworkPeer) stateConnected(ptpc *PeerToPeer) error {

	if np.RemoteState == PeerStateDisconnect {
		Log(Info, "Peer %s started disconnect procedure", np.ID)
		np.SetState(PeerStateDisconnect, ptpc)
		return nil
	}
	if np.RemoteState == PeerStateStop {
		Log(Info, "Peer %s has been stopped", np.ID)
		np.SetState(PeerStateDisconnect, ptpc)
		return nil
	}
	if np.RemoteState == PeerStateInit {
		Log(Info, "Remote peer %s decided to reconnect", np.ID)
		np.SetState(PeerStateInit, ptpc)
		return nil
	}

	if np.PeerHW == nil || np.PeerLocalIP == nil {
		np.SetState(PeerStateDisconnect, ptpc)
		return nil
	}

	if time.Since(np.LastContact) > time.Duration(time.Millisecond*3000) {
		np.LastContact = time.Now()
		for _, ep := range np.Endpoints {
			payload := []byte(ep.Addr.String())
			msg, err := ptpc.CreateMessage(MsgTypeXpeerPing, payload)
			if err != nil {
				continue
			}
			ptpc.UDPSocket.SendMessage(msg, ep.Addr)
		}
	}

	np.SetState(PeerStateRouting, ptpc)
	// if np.PingCount > 15 {
	// 	np.LastError = "Disconnected by timeout"
	// 	np.SetState(PeerStateDisconnect, ptpc)
	// 	np.PeerAddr = nil
	// 	np.Endpoint = nil
	// 	np.PingCount = 0
	// 	return fmt.Errorf("Peer %s has been timed out", np.ID)
	// }

	// if np.Endpoint == nil {
	// 	np.SetState(PeerStateDisconnect, ptpc)
	// 	np.PeerAddr = nil
	// 	np.PingCount = 0
	// 	return fmt.Errorf("Peer %s has lost endpoint", np.ID)
	// }

	// passed := time.Since(np.LastContact)
	// if passed > PeerPingTimeout {
	// 	np.LastError = ""
	// 	np.LastContact = time.Now()
	// 	Log(Trace, "Sending ping")
	// 	msg := CreateXpeerPingMessage(ptpc.Crypter, PingReq, ptpc.Interface.GetHardwareAddress().String())
	// 	ptpc.SendTo(np.PeerHW, msg)
	// 	np.PingCount++
	// }

	// Anyway we are trying to establish direct connection over time
	// if np.IsUsingTURN && len(np.KnownIPs) > 0 {
	// 	tm := CreateTestP2PMessage(ptpc.Crypter, ptpc.Dht.ID, 0)
	// 	ptpc.UDPSocket.SendMessage(tm, np.KnownIPs[0])
	// 	Log(Trace, "Sending packet directly to %s", np.KnownIPs[0].String())
	// }
	return nil
}

// stateDisconnect is executed when we've lost or terminated connection with a peer
func (np *NetworkPeer) stateDisconnect(ptpc *PeerToPeer) error {
	Log(Info, "Disconnecting %s", np.ID)
	np.SetState(PeerStateStop, ptpc)
	// TODO: Send stop to DHT
	return nil
}

// stateStop is executed when we've terminated connection with a peer
func (np *NetworkPeer) stateStop(ptpc *PeerToPeer) error {
	Log(Info, "Peer %s has been stopped", np.ID)
	return nil
}

// Utilities functions

// RequestForwarder sends a request for a proxy with DHT client
func (np *NetworkPeer) RequestForwarder(ptpc *PeerToPeer) {
	ptpc.Dht.sendRequestProxy(np.ID)
}

// ProbeLocalConnection will try to connect to every known IP addr
// over local network interface
func (np *NetworkPeer) ProbeLocalConnection(ptpc *PeerToPeer) bool {
	interfaces, err := net.Interfaces()
	if err != nil {
		Log(Error, "Failed to retrieve list of network interfaces in the system")
		return false
	}

	for _, inf := range interfaces {
		if np.Endpoint != nil {
			Log(Info, "Endpoint already set")
			break
		}
		if inf.Name == ptpc.Interface.GetName() {
			continue
		}
		addrs, _ := inf.Addrs()
		for _, addr := range addrs {
			netip, network, _ := net.ParseCIDR(addr.String())
			if !netip.IsGlobalUnicast() {
				continue
			}
			for _, kip := range np.KnownIPs {
				Log(Debug, "Probing new IP %s against network %s", kip.IP.String(), network.String())
				if network.Contains(kip.IP) {
					result := np.holePunch(kip, ptpc)
					if result {
						np.Endpoint = kip
						Log(Info, "Setting endpoint for %s to %s", np.ID, kip.String())
						return true
					}
				}
			}
		}
	}
	return false
}

func (np *NetworkPeer) sendHandshake(ptpc *PeerToPeer, proxy bool) error {
	Log(Debug, "Preparing introduction message for %s", np.ID)
	if ptpc.Dht.ID == "" {
		np.LastError = "DHT Disconnected"
		return fmt.Errorf("ID is not set")
	}
	msg := CreateIntroRequest(ptpc.Crypter, ptpc.Dht.ID)
	if proxy {
		msg.Header.ProxyID = 1
	}
	_, err := ptpc.UDPSocket.SendMessage(msg, np.Endpoint)
	if err != nil {
		np.LastError = "Failed to send intoduction message"
		Log(Error, "Failed to send introduction to %s", np.Endpoint.String())
		return fmt.Errorf("Failed to send introduction to %s", np.Endpoint)
	}
	Log(Info, "Sent introduction handshake to %s [%s %d]", np.ID, np.Endpoint.String(), np.ProxyID)
	return nil
}

// SendProxyHandshake sends a handshake packet to a proxy
func (np *NetworkPeer) SendProxyHandshake(ptpc *PeerToPeer) error {
	if np.PeerAddr == nil {
		for !np.SetPeerAddr() {
			time.Sleep(time.Millisecond * 100)
		}
	}
	Log(Info, "Handshaking with proxy %s for %s", np.Forwarder.String(), np.ID)
	msg := CreateProxyP2PMessage(-1, np.PeerAddr.String(), uint16(ptpc.UDPSocket.GetPort()))
	_, err := ptpc.UDPSocket.SendMessage(msg, np.Forwarder)
	if err != nil {
		a := np.Forwarder
		np.Forwarder = nil
		np.SetState(PeerStateWaitingForwarder, ptpc)
		np.LastError = "Failed to send handshake to a forwarder"
		return fmt.Errorf("%s failed to send handshake to a proxy %s: %v", np.ID, a.String(), err)
	}
	return nil
}

func (np *NetworkPeer) holePunch(endpoint *net.UDPAddr, ptpc *PeerToPeer) bool {
	if len(ptpc.Dht.ID) != 36 {
		Log(Error, "No personal ID. Aborting connection")
		np.SetState(PeerStateStop, ptpc)
		return false
	}
	ptpc.HolePunching.Lock()
	defer ptpc.HolePunching.Unlock()
	Log(Info, "Starting UDP hole punching to %s", endpoint.String())
	if endpoint == nil {
		Log(Error, "Endpoint is not set")
		return false
	}

	punchStarted := time.Now()
	c := uint16(0)

	for np.State == PeerStateConnectingDirectly || np.State == PeerStateConnectingInternet {
		if np.TestPacketReceived {
			np.TestPacketReceived = false
			return true
		}

		msg := CreateTestP2PMessage(ptpc.Crypter, ptpc.Dht.ID, c)
		packet := msg.Serialize()
		c++
		if c > 99 {
			c = 0
		}

		if endpoint.IP == nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}
		n, err := ptpc.UDPSocket.SendRawBytes(packet, endpoint)
		if err != nil {
			Log(Error, "Failed to send data: %s", err)
			break
		}

		Log(Trace, "Sending %d bytes. Sent %d. Endpoint: %s", len(packet), n, endpoint.String())
		passed := time.Since(punchStarted)
		if passed > time.Duration(10*time.Second) {
			Log(Warning, "Stopping UDP hole punching to %s after timeout", endpoint.String())
			break
		}

		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// SetPeerAddr will update peer address
func (np *NetworkPeer) SetPeerAddr() bool {
	if len(np.KnownIPs) == 0 {
		return false
	}
	Log(Info, "Setting peer address as %s for %s", np.KnownIPs[0].String(), np.ID)
	np.PeerAddr = np.KnownIPs[0]
	return true
}

// New states. Experimental

// Run hope punching in a separate goroutine and switch to
// Routing/Connected mode
func (np *NetworkPeer) stateConnecting(ptpc *PeerToPeer) error {
	go func() {
		round := 0
		for round < 10 {
			for _, ep := range np.KnownIPs {
				payload := []byte(ptpc.Dht.ID + ep.String())
				msg, err := ptpc.CreateMessage(MsgTypeIntroReq, payload)
				if err != nil {
					continue
				}
				_, err = ptpc.UDPSocket.SendMessage(msg, ep)
				if err != nil {
					continue
				}
				time.Sleep(time.Millisecond * 5)
			}
			time.Sleep(time.Millisecond * 20)
		}
	}()
	np.SetState(PeerStateRouting, ptpc)
	return nil
}

func (np *NetworkPeer) stateRequestingProxy(ptpc *PeerToPeer) error {
	ptpc.Dht.sendRequestProxy(np.ID)
	np.SetState(PeerStateWaitingForProxy, ptpc)
	return nil
}

func (np *NetworkPeer) stateWaitingForProxy(ptpc *PeerToPeer) error {
	started := time.Now()
	for time.Since(started) < time.Duration(time.Millisecond*4000) {
		time.Sleep(time.Millisecond * 100)
	}
	np.SetState(PeerStateConnecting, ptpc)
	return nil
}

func (np *NetworkPeer) stateWaitingToConnect(ptpc *PeerToPeer) error {
	Log(Info, "Waiting for other peer to join connection state")
	started := time.Now()
	for {
		if np.State != PeerStateWaitingToConnect {
			return nil
		}
		if np.RemoteState == PeerStateWaitingToConnect || np.RemoteState == PeerStateConnecting {
			np.SetState(PeerStateConnecting, ptpc)
			break
		}
		time.Sleep(10 * time.Millisecond)
		passed := time.Since(started)
		if passed > time.Duration(1*time.Minute) {
			np.SetState(PeerStateDisconnect, ptpc)
			return fmt.Errorf("Wait for connection failed: Peer doesn't responded in a timely manner")
		}
	}
	return nil
}

func (np *NetworkPeer) stateRouting(ptpc *PeerToPeer) error {
	locals := []PeerEndpoint{}
	internet := []PeerEndpoint{}
	proxies := []PeerEndpoint{}
	for _, ep := range np.Endpoints {
		if time.Since(ep.LastContact) > time.Duration(time.Millisecond*10) {
			continue
		}
		// Check if it's proxy
		isProxy := false
		for _, proxy := range np.Proxies {
			if proxy.String() == ep.Addr.String() {
				isProxy = true
				break
			}
		}
		if isProxy {
			proxies = append(proxies, ep)
			continue
		}
		// Check if it's LAN
		rc, err := isPrivateIP(ep.Addr.IP)
		if err != nil {
			continue
		}
		if rc {
			locals = append(locals, ep)
			continue
		}
		// Add as Internet Endpoint
		internet = append(internet, ep)
	}
	np.Endpoints = np.Endpoints[:0]
	np.Endpoints = append(np.Endpoints, locals...)
	np.Endpoints = append(np.Endpoints, internet...)
	np.Endpoints = append(np.Endpoints, proxies...)
	if len(np.Endpoints) > 0 {
		np.Endpoint = np.Endpoints[0].Addr
		np.SetState(PeerStateConnected, ptpc)
	} else {
		np.SetState(PeerStateDisconnect, ptpc)
	}
	return nil
}
