package discovery

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/H-strangeone/lan-suite/internal/ccn"
	"github.com/H-strangeone/lan-suite/internal/config"
	"github.com/H-strangeone/lan-suite/internal/crypto"
	"github.com/H-strangeone/lan-suite/internal/transport"
)

const (
	announceInterval  = 5 * time.Second
	peerTimeout       = 15 * time.Second
	cleanupInterval   = 3 * time.Second
	bootstrapInterval = 30 * time.Second
)

type Announcement struct {
	NodeID      string    `json:"node_id"`
	DisplayName string    `json:"display_name"`
	Services    []string  `json:"services"`
	HTTPPort    int       `json:"http_port"`
	QUICPort    int       `json:"quic_port"`
	PublicKey   []byte    `json:"public_key"`
	Signature   []byte    `json:"signature"`
	SentAt      time.Time `json:"sent_at"`
}

func (a *Announcement) signingPayload() []byte {
	p := struct {
		NodeID      string    `json:"node_id"`
		DisplayName string    `json:"display_name"`
		Services    []string  `json:"services"`
		HTTPPort    int       `json:"http_port"`
		QUICPort    int       `json:"quic_port"`
		PublicKey   []byte    `json:"public_key"`
		SentAt      time.Time `json:"sent_at"`
	}{
		NodeID: a.NodeID, DisplayName: a.DisplayName,
		Services: a.Services, HTTPPort: a.HTTPPort,
		QUICPort: a.QUICPort, PublicKey: a.PublicKey, SentAt: a.SentAt,
	}
	b, _ := json.Marshal(p)
	return b
}

type Peer struct {
	NodeID      string
	DisplayName string
	Services    []string
	IP          net.IP
	HTTPPort    int
	QUICPort    int
	PublicKey   ed25519.PublicKey
	FirstSeen   time.Time
	LastSeen    time.Time
	Online      bool
}

func (p *Peer) HTTPAddr() string { return fmt.Sprintf("http://%s:%d", p.IP, p.HTTPPort) }
func (p *Peer) QUICAddr() string { return fmt.Sprintf("%s:%d", p.IP, p.QUICPort) }

type Discoverer struct {
	cfg      *config.Config
	identity *crypto.Identity
	router   *ccn.Router
	hub      *transport.Hub

	mu    sync.RWMutex
	peers map[string]*Peer

	conn   *net.UDPConn
	stopCh chan struct{}
}

func New(cfg *config.Config, id *crypto.Identity, router *ccn.Router, hub *transport.Hub) *Discoverer {
	return &Discoverer{
		cfg:      cfg,
		identity: id,
		router:   router,
		hub:      hub,
		peers:    make(map[string]*Peer),
		stopCh:   make(chan struct{}),
	}
}

func (d *Discoverer) Start() error {
	addr, err := net.ResolveUDPAddr("udp4",
		fmt.Sprintf("%s:%d", d.cfg.MulticastGroup, d.cfg.MulticastPort))
	if err != nil {
		return fmt.Errorf("resolving multicast addr: %w", err)
	}

	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("joining multicast group %s: %w", addr, err)
	}
	conn.SetReadBuffer(4 * 1024 * 1024)
	d.conn = conn

	log.Printf("[discovery] joined multicast group %s", addr)

	go d.announceLoop()
	go d.listenLoop()
	go d.cleanupLoop()
	if len(d.cfg.BootstrapPeers) > 0 {
		go d.bootstrapLoop()
		log.Printf("[discovery] bootstrap peers: %v", d.cfg.BootstrapPeers)
	}
	return nil
}

func (d *Discoverer) Stop() {
	close(d.stopCh)
	if d.conn != nil {
		d.conn.Close()
	}
}

func (d *Discoverer) Peers() []*Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	out := make([]*Peer, 0, len(d.peers))
	for _, p := range d.peers {
		if p.Online {
			out = append(out, p)
		}
	}
	return out
}

func (d *Discoverer) GetPeer(nodeID string) *Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.peers[nodeID]
}

func (d *Discoverer) SelfAnnouncement() *Announcement {
	ann := &Announcement{
		NodeID:      d.identity.NodeID,
		DisplayName: d.cfg.NodeName,
		Services:    []string{"chat", "video", "drive"},
		HTTPPort:    d.cfg.Port,
		QUICPort:    d.cfg.QUICPort,
		PublicKey:   []byte(d.identity.PublicKey),
		SentAt:      time.Now(),
	}
	ann.Signature = d.identity.Sign(ann.signingPayload())
	return ann
}

func (d *Discoverer) HandleRemoteAnnouncement(ann Announcement, ip net.IP) {
	data, err := json.Marshal(ann)
	if err != nil {
		return
	}
	d.handleAnnouncement(data, ip)
}

func (d *Discoverer) announceLoop() {

	d.sendAnnouncement()

	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.sendAnnouncement()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Discoverer) sendAnnouncement() {
	ann := &Announcement{
		NodeID:      d.identity.NodeID,
		DisplayName: d.cfg.NodeName,
		Services:    []string{"chat", "video", "drive"},
		HTTPPort:    d.cfg.Port,
		QUICPort:    d.cfg.QUICPort,
		PublicKey:   []byte(d.identity.PublicKey),
		SentAt:      time.Now(),
	}
	ann.Signature = d.identity.Sign(ann.signingPayload())

	data, err := json.Marshal(ann)
	if err != nil {
		log.Printf("[discovery] marshal error: %v", err)
		return
	}

	addr, _ := net.ResolveUDPAddr("udp4",
		fmt.Sprintf("%s:%d", d.cfg.MulticastGroup, d.cfg.MulticastPort))

	if _, err := d.conn.WriteTo(data, addr); err != nil {
		log.Printf("[discovery] send error: %v", err)
	}
}

func (d *Discoverer) listenLoop() {
	buf := make([]byte, 4096)
	for {
		n, senderAddr, err := d.conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-d.stopCh:
				return
			default:
				log.Printf("[discovery] read error: %v", err)
				continue
			}
		}
		raw := make([]byte, n)
		copy(raw, buf[:n])

		go d.handleAnnouncement(raw, senderAddr.IP)
	}
}

func (d *Discoverer) handleAnnouncement(data []byte, senderIP net.IP) {
	var ann Announcement
	if err := json.Unmarshal(data, &ann); err != nil {
		return
	}

	if ann.NodeID == d.identity.NodeID {
		return
	}

	if ann.NodeID == "" || ann.DisplayName == "" || len(ann.Signature) == 0 {
		log.Printf("[discovery] malformed announcement from %s", senderIP)
		return
	}

	if len(ann.PublicKey) != ed25519.PublicKeySize {
		log.Printf("[discovery] invalid pubkey size from %s", senderIP)
		return
	}

	pubKey := ed25519.PublicKey(ann.PublicKey)
	if !ed25519.Verify(pubKey, ann.signingPayload(), ann.Signature) {
		log.Printf("[discovery] bad signature from %s (claimed: %s)", senderIP, ann.NodeID[:8])
		return
	}

	if ann.NodeID != crypto.HashBytes(ann.PublicKey) {
		log.Printf("[discovery] nodeID/pubkey mismatch from %s", senderIP)
		return
	}

	if time.Since(ann.SentAt) > 30*time.Second {
		log.Printf("[discovery] stale announcement from %s (age=%v)", senderIP, time.Since(ann.SentAt))
		return
	}

	d.upsertPeer(&ann, senderIP)
}

func (d *Discoverer) upsertPeer(ann *Announcement, ip net.IP) {
	d.mu.Lock()

	existing, known := d.peers[ann.NodeID]

	if !known {
		peer := &Peer{
			NodeID:      ann.NodeID,
			DisplayName: ann.DisplayName,
			Services:    ann.Services,
			IP:          ip,
			HTTPPort:    ann.HTTPPort,
			QUICPort:    ann.QUICPort,
			PublicKey:   ed25519.PublicKey(ann.PublicKey),
			FirstSeen:   time.Now(),
			LastSeen:    time.Now(),
			Online:      true,
		}
		d.peers[ann.NodeID] = peer
		d.mu.Unlock()

		log.Printf("[discovery] + peer: %s (%s) @ %s services=%v",
			ann.DisplayName, ann.NodeID[:8], ip, ann.Services)
		d.onPeerJoined(peer)
		return
	}

	existing.LastSeen = time.Now()
	existing.IP = ip
	existing.DisplayName = ann.DisplayName
	existing.Services = ann.Services
	wasOffline := !existing.Online
	existing.Online = true
	d.mu.Unlock()

	if wasOffline {
		log.Printf("[discovery] peer back: %s (%s)", ann.DisplayName, ann.NodeID[:8])
		d.onPeerJoined(existing)
	}
}

func (d *Discoverer) onPeerJoined(peer *Peer) {
	if d.router != nil {
		d.router.AddRoute(ccn.Name{"chat"}, peer.NodeID)
		d.router.AddRoute(ccn.Name{"drive", peer.NodeID}, peer.NodeID)
		d.router.AddRoute(ccn.Name{"file"}, peer.NodeID)
		d.router.AddRoute(ccn.Name{"video"}, peer.NodeID)
	}
	if d.hub != nil {
		d.hub.NotifyPeerJoined(peer.NodeID, peer.DisplayName, peer.Services)
	}
}

func (d *Discoverer) onPeerLeft(peer *Peer) {
	log.Printf("[discovery] - peer: %s (%s)", peer.DisplayName, peer.NodeID[:8])
	if d.router != nil {
		d.router.RemoveRoute(ccn.Name{"chat"}, peer.NodeID)
		d.router.RemoveRoute(ccn.Name{"drive", peer.NodeID}, peer.NodeID)
		d.router.RemoveRoute(ccn.Name{"file"}, peer.NodeID)
		d.router.RemoveRoute(ccn.Name{"video"}, peer.NodeID)
	}
	if d.hub != nil {
		d.hub.NotifyPeerLeft(peer.NodeID)
	}
}

func (d *Discoverer) cleanupLoop() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.checkTimeouts()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Discoverer) checkTimeouts() {
	now := time.Now()
	d.mu.Lock()
	var gone []*Peer
	for _, p := range d.peers {
		if p.Online && now.Sub(p.LastSeen) > peerTimeout {
			p.Online = false
			gone = append(gone, p)
		}
	}
	d.mu.Unlock()

	for _, p := range gone {
		d.onPeerLeft(p)
	}
}

func (d *Discoverer) bootstrapLoop() {

	d.contactAllBootstrapPeers()

	ticker := time.NewTicker(bootstrapInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.contactAllBootstrapPeers()
		case <-d.stopCh:
			return
		}
	}
}

func (d *Discoverer) contactAllBootstrapPeers() {
	for _, addr := range d.cfg.BootstrapPeers {
		go d.contactBootstrapPeer(addr)
	}
}

func (d *Discoverer) contactBootstrapPeer(addr string) {

	if !strings.HasPrefix(addr, "http") {
		addr = "http://" + addr
	}

	client := &http.Client{Timeout: 5 * time.Second}

	resp, err := client.Get(addr + "/api/discovery/announce")
	if err != nil {
		log.Printf("[discovery] bootstrap %s unreachable: %v", addr, err)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("[discovery] bootstrap %s bad response: status=%d", addr, resp.StatusCode)
		return
	}

	var ann Announcement
	if err := json.Unmarshal(body, &ann); err != nil {
		log.Printf("[discovery] bootstrap %s bad announcement: %v", addr, err)
		return
	}

	host := addr
	host = strings.TrimPrefix(host, "http://")
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			log.Printf("[discovery] bootstrap %s: cannot resolve IP", addr)
			return
		}
		ip = net.ParseIP(addrs[0])
	}

	d.handleAnnouncement(body, ip)
// Reuse the same verification path as multicast announcements
	d.handleAnnouncement(body, ip)

	// 2. Push our own announcement to them so they know about us too
	ann2 := &Announcement{
		NodeID:      d.identity.NodeID,
		DisplayName: d.cfg.NodeName,
		Services:    []string{"chat", "video", "drive"},
		HTTPPort:    d.cfg.Port,
		QUICPort:    d.cfg.QUICPort,
		PublicKey:   []byte(d.identity.PublicKey),
		SentAt:      time.Now(),
	}
	ann2.Signature = d.identity.Sign(ann2.signingPayload())
	data, err := json.Marshal(ann2)
	if err != nil {
		return
	}

	resp2, err := client.Post(
		addr+"/api/discovery/announce",
		"application/json",
		strings.NewReader(string(data)),
	)
	if err == nil {
		resp2.Body.Close()
	}
}