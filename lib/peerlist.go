package ptp

import (
	"fmt"
	"net"
	"sync"
)

type ListOperation int

const (
	PeersDelete ListOperation = 0
	PeersUpdate ListOperation = 1
)

// PeerList is for handling list of peers with all mappings
type PeerList struct {
	peers      map[string]*NetworkPeer
	tableIPID  map[string]string // Mapping for IP->ID
	tableMacID map[string]string // Mapping for MAC->ID
	lock       sync.RWMutex
}

// Init will initialize PeerList's maps
func (l *PeerList) Init() {
	l.peers = make(map[string]*NetworkPeer)
	l.tableIPID = make(map[string]string)
	l.tableMacID = make(map[string]string)
}

func (l *PeerList) operate(action ListOperation, id string, peer *NetworkPeer) {
	l.lock.Lock()
	defer l.lock.Unlock()
	if action == PeersUpdate {
		l.peers[id] = peer
		ip := ""
		mac := ""
		if peer.PeerLocalIP != nil {
			ip = peer.PeerLocalIP.String()
		}
		if peer.PeerHW != nil {
			mac = peer.PeerHW.String()
		}
		l.updateTables(id, ip, mac)
	} else if action == PeersDelete {
		peer, exists := l.peers[id]
		if !exists {
			return
		}
		l.deleteTables(peer.PeerLocalIP.String(), peer.PeerHW.String())
		delete(l.peers, id)
		return
	}
}

func (l *PeerList) updateTables(id, ip, mac string) {
	if ip != "" {
		l.tableIPID[ip] = id
	}
	if mac != "" {
		l.tableMacID[mac] = id
	}
}

func (l *PeerList) deleteTables(ip, mac string) {
	if ip != "" {
		_, exists := l.tableIPID[ip]
		if exists {
			delete(l.tableIPID, ip)
		}
	}
	if mac != "" {
		_, exists := l.tableMacID[mac]
		if exists {
			delete(l.tableMacID, mac)
		}
	}
}

func (l *PeerList) Delete(id string) {
	l.operate(PeersDelete, id, nil)
}

// Update will append/edit peer in list
func (l *PeerList) Update(id string, peer *NetworkPeer) {
	l.operate(PeersUpdate, id, peer)
}

// Get returns copy of map with all peers
func (l *PeerList) Get() map[string]*NetworkPeer {
	result := make(map[string]*NetworkPeer)
	l.lock.RLock()
	for id, peer := range l.peers {
		result[id] = peer
	}
	l.lock.RUnlock()
	return result
}

// GetPeer returns single peer by id
func (l *PeerList) GetPeer(id string) *NetworkPeer {
	l.lock.RLock()
	peer, exists := l.peers[id]
	l.lock.RUnlock()
	if exists {
		return peer
	}
	return nil
}

// GetEndpointAndProxy returns endpoint address and proxy id
func (l *PeerList) GetEndpointAndProxy(mac string) (*net.UDPAddr, uint16, error) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	id, exists := l.tableMacID[mac]
	if exists {
		return l.peers[id].Endpoint, uint16(l.peers[id].ProxyID), nil
	}
	return nil, 0, fmt.Errorf("Specified hardware address was not found in table")
}

// GetID returns ID by specified IP
func (l *PeerList) GetID(ip string) (string, error) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	id, exists := l.tableIPID[ip]
	if exists {
		return id, nil
	}
	return "", fmt.Errorf("Specified IP was not found in table")
}

func (l *PeerList) Length() int {
	return len(l.peers)
}

func (l *PeerList) RunPeer(id string, p *PeerToPeer) {
	l.lock.RLock()
	defer l.lock.RUnlock()
	go l.peers[id].Run(p)
}
