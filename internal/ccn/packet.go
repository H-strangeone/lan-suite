package ccn// Package ccn implements a simplified Content-Centric Networking layer.
// Instead of addressing by location (IP:port), content is addressed by NAME.
// Any node that has the content can serve it — not just the original source.
/*
  CONCEPT: Content-Centric Networking (CCN) — the full picture
  ──────────────────────────────────────────────────────────────
  Traditional networking asks: "WHERE is the content?"
    → Connect to 192.168.1.5:8080, GET /thesis.pdf

  CCN asks: "WHAT content do I want?"
    → Interest("/drive/alice/thesis.pdf")
    → Any node that has it can respond

  WHY THIS IS POWERFUL FOR A LAN SUITE:
  1. Automatic caching: if 10 people request the same file,
     only the first request goes all the way to the source.
     All 10 might be served by a nearby peer's cache.

  2. Multi-source: a large file can be fetched from multiple peers
     simultaneously — each serving different chunks.
     This is why BitTorrent is fast. We're building that in.

  3. Named messages: chat messages are content items with names like
     /chat/room1/messages/42. Peers can "catch up" by requesting
     messages they missed while offline, by name.

  4. Offline-first: if a peer is offline when you send a message,
     the message sits in YOUR content store. When they reconnect,
     they issue interests and pull the messages they missed.

  NAMING SCHEME:
  Names are hierarchical, slash-separated, like file paths.
  Components are the path segments.

  Our namespace:
  /chat/<roomID>/messages/<seqno>          chat messages
  /chat/<roomID>/members                   room member list
  /video/calls/<callID>/offer              WebRTC SDP offer
  /video/calls/<callID>/answer             WebRTC SDP answer
  /video/calls/<callID>/ice/<idx>          ICE candidate
  /file/<fileHash>/info                    file metadata
  /file/<fileHash>/chunk/<idx>             file chunk
  /drive/<nodeID>/<path...>                distributed drive
  /discovery/peers/<nodeID>               peer announcements
  /discovery/rooms                         room listings
*/

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Name is a CCN content name — a hierarchical identifier.
// Stored as a slice of components for efficient prefix matching.
// "/chat/room1/messages/42" → Name{"chat","room1","messages","42"}
type Name []string

// ParseName parses a slash-separated name string into a Name.
// Leading/trailing slashes are stripped. Empty components are removed.
func ParseName(s string) Name {
	parts := strings.Split(strings.Trim(s, "/"), "/")
	out := make(Name, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// String returns the canonical string form: "/chat/room1/messages/42"
func (n Name) String() string {
	if len(n) == 0 {
		return "/"
	}
	return "/" + strings.Join(n, "/")
}

// HasPrefix returns true if this name starts with the given prefix.
// Used by the FIB for longest-prefix matching.
// Name{"chat","room1","messages","42"}.HasPrefix(Name{"chat","room1"}) → true
func (n Name) HasPrefix(prefix Name) bool {
	if len(prefix) > len(n) {
		return false
	}
	for i, component := range prefix {
		if n[i] != component {
			return false
		}
	}
	return true
}

// Equal returns true if two names are identical.
func (n Name) Equal(other Name) bool {
	if len(n) != len(other) {
		return false
	}
	for i := range n {
		if n[i] != other[i] {
			return false
		}
	}
	return true
}

// Key returns a string suitable for use as a map key.
// We can't use Name (a slice) as a map key directly — slices aren't comparable.
// String is fine for map keys.
func (n Name) Key() string {
	return n.String()
}

// ── Interest Packet ───────────────────────────────────────────────────────────

/*
  CONCEPT: Interest Packet
  ─────────────────────────
  An Interest is a REQUEST for content.
  "I want the content named X."

  Fields:
  - Name: what content you want
  - Nonce: random bytes to distinguish duplicate interests
    (if two nodes ask for the same name, the router needs to tell them apart)
  - CanBePrefix: true = any content whose name STARTS with Name is acceptable
    e.g. Interest("/file/abc/chunk") with CanBePrefix=true matches
    /file/abc/chunk/0, /file/abc/chunk/1, etc.
  - MustBeFresh: true = don't serve stale cached data
  - InterestLifetimeMs: how long to wait for a response before giving up
  - ForwardedBy: which peer forwarded this interest to us (for loop detection)
*/

// Interest is a CCN Interest packet — a request for named content.
type Interest struct {
	Name                Name     `json:"name"`
	Nonce               string   `json:"nonce"`            // random hex, deduplication
	CanBePrefix         bool     `json:"can_be_prefix"`    // prefix match ok?
	MustBeFresh         bool     `json:"must_be_fresh"`    // skip stale cache?
	InterestLifetimeMs  int      `json:"lifetime_ms"`      // timeout in ms
	ForwardedBy         []string `json:"forwarded_by"`     // loop prevention hop list
}

// NewInterest creates an Interest packet with sensible defaults.
func NewInterest(name Name) *Interest {
	return &Interest{
		Name:               name,
		Nonce:              generateNonce(),
		CanBePrefix:        false,
		MustBeFresh:        false,
		InterestLifetimeMs: 4000, // 4 seconds default
		ForwardedBy:        []string{},
	}
}

// AddHop appends a nodeID to ForwardedBy.
// This is checked before forwarding to prevent routing loops.
func (i *Interest) AddHop(nodeID string) {
	i.ForwardedBy = append(i.ForwardedBy, nodeID)
}

// HasLooped returns true if nodeID already appears in ForwardedBy.
// We use this to drop interests that have come back to a node they visited.
func (i *Interest) HasLooped(nodeID string) bool {
	for _, id := range i.ForwardedBy {
		if id == nodeID {
			return true
		}
	}
	return false
}

// ── Data Packet ───────────────────────────────────────────────────────────────

/*
  CONCEPT: Data Packet
  ─────────────────────
  A Data packet is a RESPONSE — it carries the actual content.

  Fields:
  - Name: exactly what content this is (must satisfy the Interest's name)
  - Content: the raw bytes of the content
  - ContentType: MIME type or our custom types
  - FreshnessPeriodMs: how long this data is considered "fresh"
    After this expires, MustBeFresh interests won't be served from cache.
  - Signature: Ed25519 signature over (Name + Content)
    Proves the data came from the claimed producer and wasn't tampered with.
  - SignerPublicKey: the public key used to sign (so receivers can verify)
  - MetaInfo: extra key-value metadata

  CRITICAL: In CCN, ANY node that has the data can serve it (caching).
  The signature proves authenticity even when served by a third party.
  Without signatures, a malicious peer could serve modified data.
*/

// ContentType identifies the kind of content in a Data packet.
type ContentType string

const (
	ContentTypeBlob    ContentType = "blob"        // raw bytes (file chunks)
	ContentTypeJSON    ContentType = "json"        // JSON-encoded data
	ContentTypeMessage ContentType = "message"     // chat message
	ContentTypeMeta    ContentType = "meta"        // metadata/manifest
	ContentTypePeer    ContentType = "peer"        // peer announcement
)

// Data is a CCN Data packet — named, signed content.
type Data struct {
	Name              Name              `json:"name"`
	Content           []byte            `json:"content"`            // actual payload
	ContentType       ContentType       `json:"content_type"`
	FreshnessPeriodMs int               `json:"freshness_period_ms"`
	ProducerNodeID    string            `json:"producer_node_id"`   // who created this
	SignerPublicKey   ed25519.PublicKey  `json:"signer_public_key"`  // for verification
	Signature         []byte            `json:"signature"`          // Ed25519 sig
	Meta              map[string]string `json:"meta,omitempty"`     // arbitrary metadata
	CreatedAt         time.Time         `json:"created_at"`
}

// NewData creates an unsigned Data packet.
// Call Sign() before transmitting to peers.
func NewData(name Name, content []byte, contentType ContentType) *Data {
	return &Data{
		Name:              name,
		Content:           content,
		ContentType:       contentType,
		FreshnessPeriodMs: 300_000, // 5 minutes default
		CreatedAt:         time.Now(),
		Meta:              make(map[string]string),
	}
}

/*
  CONCEPT: What we sign
  ──────────────────────
  We sign: name + content + contentType + producerNodeID + createdAt
  This covers everything that identifies and authenticates the packet.
  An attacker can't change the content without invalidating the signature.
  An attacker can't change the name without invalidating the signature.
  An attacker can't replay old content as new (createdAt is signed).

  We do NOT sign: FreshnessPeriodMs, Meta — these are advisory and can
  be adjusted by caching nodes without breaking verification.
*/

// SigningPayload returns the bytes that are signed/verified.
// Both Sign() and Verify() must use the exact same payload.
func (d *Data) SigningPayload() []byte {
	/*
	  We use JSON encoding of a canonical subset of fields.
	  In production NDN, there's a TLV wire format for this.
	  JSON is easier to implement and debug while we're building.
	*/
	payload := struct {
		Name           string      `json:"name"`
		Content        []byte      `json:"content"`
		ContentType    ContentType `json:"content_type"`
		ProducerNodeID string      `json:"producer_node_id"`
		CreatedAt      time.Time   `json:"created_at"`
	}{
		Name:           d.Name.Key(),
		Content:        d.Content,
		ContentType:    d.ContentType,
		ProducerNodeID: d.ProducerNodeID,
		CreatedAt:      d.CreatedAt,
	}
	bytes, _ := json.Marshal(payload) // marshal error is impossible here
	return bytes
}

// Sign signs this Data packet using the given Ed25519 private key.
// Must be called before transmitting to peers.
func (d *Data) Sign(privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, nodeID string) {
	d.ProducerNodeID = nodeID
	d.SignerPublicKey = publicKey
	d.Signature = ed25519.Sign(privateKey, d.SigningPayload())
}

// Verify checks the Data packet's signature.
// Returns nil if valid, error if signature is missing, wrong, or tampered.
func (d *Data) Verify() error {
	if len(d.Signature) == 0 {
		return fmt.Errorf("data packet has no signature")
	}
	if len(d.SignerPublicKey) == 0 {
		return fmt.Errorf("data packet has no signer public key")
	}
	if !ed25519.Verify(d.SignerPublicKey, d.SigningPayload(), d.Signature) {
		return fmt.Errorf("invalid signature on %s", d.Name)
	}
	return nil
}

// IsFresh returns true if this data is still within its freshness period.
func (d *Data) IsFresh() bool {
	if d.FreshnessPeriodMs <= 0 {
		return true // no expiry set — always fresh
	}
	freshUntil := d.CreatedAt.Add(time.Duration(d.FreshnessPeriodMs) * time.Millisecond)
	return time.Now().Before(freshUntil)
}

// ── Wire format ───────────────────────────────────────────────────────────────

/*
  CONCEPT: Envelope for network transmission
  ───────────────────────────────────────────
  When we send a CCN packet over WebSocket or UDP, we need to know:
  "Is this an Interest or a Data packet?"

  We wrap both in a Packet envelope with a Type field.
  This is the same discriminated union pattern as our WebSocket messages.

  On the wire (JSON):
  {
    "type": "interest",
    "interest": { "name": ["chat","room1","msg","42"], ... }
  }
  or:
  {
    "type": "data",
    "data": { "name": [...], "content": "...", "signature": "...", ... }
  }
*/

// PacketType distinguishes Interest from Data on the wire.
type PacketType string

const (
	PacketTypeInterest PacketType = "interest"
	PacketTypeData     PacketType = "data"
)

// Packet is the wire envelope for all CCN packets.
type Packet struct {
	Type     PacketType `json:"type"`
	Interest *Interest  `json:"interest,omitempty"`
	Data     *Data      `json:"data,omitempty"`
}

// WrapInterest creates a Packet envelope for an Interest.
func WrapInterest(i *Interest) *Packet {
	return &Packet{Type: PacketTypeInterest, Interest: i}
}

// WrapData creates a Packet envelope for a Data packet.
func WrapData(d *Data) *Packet {
	return &Packet{Type: PacketTypeData, Data: d}
}

// ── Name constructors for our namespace ──────────────────────────────────────
// These functions build well-formed names for each content type.
// Using constructors prevents typos in name strings scattered everywhere.

// ChatMsgName builds /chat/<roomID>/messages/<seqno>
func ChatMsgName(roomID string, seqno uint64) Name {
	return Name{"chat", roomID, "messages", fmt.Sprintf("%d", seqno)}
}

// ChatRoomName builds /chat/<roomID>/members
func ChatRoomName(roomID string) Name {
	return Name{"chat", roomID, "members"}
}

// VideoOfferName builds /video/calls/<callID>/offer
func VideoOfferName(callID string) Name {
	return Name{"video", "calls", callID, "offer"}
}

// VideoAnswerName builds /video/calls/<callID>/answer
func VideoAnswerName(callID string) Name {
	return Name{"video", "calls", callID, "answer"}
}

// VideoICEName builds /video/calls/<callID>/ice/<idx>
func VideoICEName(callID string, idx int) Name {
	return Name{"video", "calls", callID, "ice", fmt.Sprintf("%d", idx)}
}

// FileInfoName builds /file/<fileHash>/info
func FileInfoName(fileHash string) Name {
	return Name{"file", fileHash, "info"}
}

// FileChunkName builds /file/<fileHash>/chunk/<idx>
func FileChunkName(fileHash string, chunkIdx int) Name {
	return Name{"file", fileHash, "chunk", fmt.Sprintf("%d", chunkIdx)}
}

// DriveName builds /drive/<nodeID>/<path components...>
func DriveName(nodeID string, pathComponents ...string) Name {
	n := Name{"drive", nodeID}
	return append(n, pathComponents...)
}

// PeerAnnounceName builds /discovery/peers/<nodeID>
func PeerAnnounceName(nodeID string) Name {
	return Name{"discovery", "peers", nodeID}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// generateNonce creates a random 8-char hex string for Interest deduplication.
func generateNonce() string {
	// We use time-based nonce here to avoid importing crypto/rand in this package.
	// In production, use crypto/rand. Good enough for deduplication.
	return fmt.Sprintf("%x", time.Now().UnixNano())
}