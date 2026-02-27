package discovery

/*
  CONCEPT: UDP Multicast
  ───────────────────────
  Normal UDP unicast: you send to ONE specific IP.
  Broadcast (255.255.255.255): every device on LAN receives it.
    Problem: routers don't forward broadcasts between subnets.
  Multicast (224.x.x.x): only devices that JOINED the group receive it.
    Better: routers can forward it, more efficient than broadcast.

  We use 224.0.0.251:5353 — same group as mDNS.
  Our payload is JSON, not DNS — but the transport is identical.

  FLOW:
  Every node runs two goroutines:
  - announceLoop: sends our presence every 5 seconds
  - listenLoop:   receives announcements from other nodes

  When we hear a new peer:
  1. Verify Ed25519 signature (they are who they claim to be)
  2. Verify NodeID == SHA256(PublicKey) (they own that key)
  3. Check SentAt timestamp (reject replays older than 30s)
  4. Add to peer table
  5. Add CCN FIB routes (/chat, /file, /drive/<nodeID> → that peer)
  6. Tell the WebSocket hub → frontend sees the peer appear instantly

  When a peer stops announcing for 15s:
  1. Mark offline
  2. Remove CCN routes
  3. Tell the hub → frontend sees peer disappear
*/

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"github.com/H-strangeone/lan-suite/internal/ccn"
	"github.com/H-strangeone/lan-suite/internal/config"
	"github.com/H-strangeone/lan-suite/internal/crypto"
	"github.com/H-strangeone/lan-suite/internal/transport"
)

const (
	announceInterval = 5 * time.Second
	peerTimeout      = 15 * time.Second
	cleanupInterval  = 3 * time.Second
)

// Announcement is the UDP multicast payload every node broadcasts.
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

// signingPayload returns the bytes that are signed/verified.
// Must be identical on both the signer and verifier side.
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

// Peer is a discovered LAN node.
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

// Discoverer manages peer announcement and discovery.
type Discoverer struct {
	cfg      *config.Config
	identity *crypto.Identity
	router   *ccn.Router
	hub      *transport.Hub

	mu     sync.RWMutex
	peers  map[string]*Peer

	conn   *net.UDPConn
	stopCh chan struct{}
}

// New creates a Discoverer. router and hub may be nil (discovery still works).
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

// Start opens the UDP multicast socket and launches all goroutines.
func (d *Discoverer) Start() error {
	addr, err := net.ResolveUDPAddr("udp4",
		fmt.Sprintf("%s:%d", d.cfg.MulticastGroup, d.cfg.MulticastPort))
	if err != nil {
		return fmt.Errorf("resolving multicast addr: %w", err)
	}

	/*
	  net.ListenMulticastUDP joins the multicast group on ALL network interfaces.
	  nil = use all interfaces (works on both WiFi and Ethernet simultaneously).
	  Pass a *net.Interface to restrict to a specific adapter.
	*/
	conn, err := net.ListenMulticastUDP("udp4", nil, addr)
	if err != nil {
		return fmt.Errorf("joining multicast group %s: %w", addr, err)
	}
	conn.SetReadBuffer(4 * 1024 * 1024) // 4MB receive buffer
	d.conn = conn

	log.Printf("[discovery] joined multicast group %s", addr)

	go d.announceLoop()
	go d.listenLoop()
	go d.cleanupLoop()
	return nil
}

// Stop shuts down discovery.
func (d *Discoverer) Stop() {
	close(d.stopCh)
	if d.conn != nil {
		d.conn.Close()
	}
}

// Peers returns a snapshot of all online peers.
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

// GetPeer returns a peer by nodeID, or nil if unknown.
func (d *Discoverer) GetPeer(nodeID string) *Peer {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.peers[nodeID]
}

// ── Announce ──────────────────────────────────────────────────────────────────

func (d *Discoverer) announceLoop() {
	// Send immediately, then on interval
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

// ── Listen ────────────────────────────────────────────────────────────────────

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
		// Process in separate goroutine — don't block the receive loop
		go d.handleAnnouncement(raw, senderAddr.IP)
	}
}

func (d *Discoverer) handleAnnouncement(data []byte, senderIP net.IP) {
	var ann Announcement
	if err := json.Unmarshal(data, &ann); err != nil {
		return // silently drop malformed packets
	}

	// Ignore our own announcements
	if ann.NodeID == d.identity.NodeID {
		return
	}

	// Validate required fields
	if ann.NodeID == "" || ann.DisplayName == "" || len(ann.Signature) == 0 {
		log.Printf("[discovery] malformed announcement from %s", senderIP)
		return
	}

	// Validate public key size
	if len(ann.PublicKey) != ed25519.PublicKeySize {
		log.Printf("[discovery] invalid pubkey size from %s", senderIP)
		return
	}

	/*
	  TWO-STEP IDENTITY VERIFICATION:

	  Step 1: Verify the Ed25519 signature.
	  "Was this announcement signed by the private key matching ann.PublicKey?"
	  If yes: the sender holds that private key.

	  Step 2: Verify NodeID == SHA256(PublicKey).
	  "Does the claimed NodeID actually correspond to ann.PublicKey?"
	  If yes: the NodeID is not spoofed.

	  Together: "This node is who it claims to be, and holds the key to prove it."
	  Without step 2: attacker could claim someone else's NodeID with their own key.
	  Without step 1: attacker could claim any NodeID without a key at all.
	*/
	pubKey := ed25519.PublicKey(ann.PublicKey)
	if !ed25519.Verify(pubKey, ann.signingPayload(), ann.Signature) {
		log.Printf("[discovery] bad signature from %s (claimed: %s)", senderIP, ann.NodeID[:8])
		return
	}

	if ann.NodeID != crypto.HashBytes(ann.PublicKey) {
		log.Printf("[discovery] nodeID/pubkey mismatch from %s", senderIP)
		return
	}

	// Reject stale announcements (replay attack prevention)
	if time.Since(ann.SentAt) > 30*time.Second {
		log.Printf("[discovery] stale announcement from %s (age=%v)", senderIP, time.Since(ann.SentAt))
		return
	}

	d.upsertPeer(&ann, senderIP)
}

// ── Peer table ────────────────────────────────────────────────────────────────

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

	// Update existing
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

// ── Cleanup ───────────────────────────────────────────────────────────────────

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

	// Callbacks outside lock — prevents deadlock if hub/router acquire their own locks
	for _, p := range gone {
		d.onPeerLeft(p)
	}
}