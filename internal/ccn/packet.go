package ccn

import (
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Name []string

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

func (n Name) String() string {
	if len(n) == 0 {
		return "/"
	}
	return "/" + strings.Join(n, "/")
}

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

func (n Name) Key() string {
	return n.String()
}

type Interest struct {
	Name               Name     `json:"name"`
	Nonce              string   `json:"nonce"`
	CanBePrefix        bool     `json:"can_be_prefix"`
	MustBeFresh        bool     `json:"must_be_fresh"`
	InterestLifetimeMs int      `json:"lifetime_ms"`
	ForwardedBy        []string `json:"forwarded_by"`
}

func NewInterest(name Name) *Interest {
	return &Interest{
		Name:               name,
		Nonce:              generateNonce(),
		CanBePrefix:        false,
		MustBeFresh:        false,
		InterestLifetimeMs: 4000,
		ForwardedBy:        []string{},
	}
}

func (i *Interest) AddHop(nodeID string) {
	i.ForwardedBy = append(i.ForwardedBy, nodeID)
}

func (i *Interest) HasLooped(nodeID string) bool {
	for _, id := range i.ForwardedBy {
		if id == nodeID {
			return true
		}
	}
	return false
}

type ContentType string

const (
	ContentTypeBlob    ContentType = "blob"
	ContentTypeJSON    ContentType = "json"
	ContentTypeMessage ContentType = "message"
	ContentTypeMeta    ContentType = "meta"
	ContentTypePeer    ContentType = "peer"
)

type Data struct {
	Name              Name              `json:"name"`
	Content           []byte            `json:"content"`
	ContentType       ContentType       `json:"content_type"`
	FreshnessPeriodMs int               `json:"freshness_period_ms"`
	ProducerNodeID    string            `json:"producer_node_id"`
	SignerPublicKey   ed25519.PublicKey `json:"signer_public_key"`
	Signature         []byte            `json:"signature"`
	Meta              map[string]string `json:"meta,omitempty"`
	CreatedAt         time.Time         `json:"created_at"`
}

func NewData(name Name, content []byte, contentType ContentType) *Data {
	return &Data{
		Name:              name,
		Content:           content,
		ContentType:       contentType,
		FreshnessPeriodMs: 300_000,
		CreatedAt:         time.Now(),
		Meta:              make(map[string]string),
	}
}

func (d *Data) SigningPayload() []byte {

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
	bytes, _ := json.Marshal(payload)
	return bytes
}

func (d *Data) Sign(privateKey ed25519.PrivateKey, publicKey ed25519.PublicKey, nodeID string) {
	d.ProducerNodeID = nodeID
	d.SignerPublicKey = publicKey
	d.Signature = ed25519.Sign(privateKey, d.SigningPayload())
}

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

func (d *Data) IsFresh() bool {
	if d.FreshnessPeriodMs <= 0 {
		return true
	}
	freshUntil := d.CreatedAt.Add(time.Duration(d.FreshnessPeriodMs) * time.Millisecond)
	return time.Now().Before(freshUntil)
}

type PacketType string

const (
	PacketTypeInterest PacketType = "interest"
	PacketTypeData     PacketType = "data"
)

type Packet struct {
	Type     PacketType `json:"type"`
	Interest *Interest  `json:"interest,omitempty"`
	Data     *Data      `json:"data,omitempty"`
}

func WrapInterest(i *Interest) *Packet {
	return &Packet{Type: PacketTypeInterest, Interest: i}
}

func WrapData(d *Data) *Packet {
	return &Packet{Type: PacketTypeData, Data: d}
}

func ChatMsgName(roomID string, seqno uint64) Name {
	return Name{"chat", roomID, "messages", fmt.Sprintf("%d", seqno)}
}

func ChatRoomName(roomID string) Name {
	return Name{"chat", roomID, "members"}
}

func VideoOfferName(callID string) Name {
	return Name{"video", "calls", callID, "offer"}
}

func VideoAnswerName(callID string) Name {
	return Name{"video", "calls", callID, "answer"}
}

func VideoICEName(callID string, idx int) Name {
	return Name{"video", "calls", callID, "ice", fmt.Sprintf("%d", idx)}
}

func FileInfoName(fileHash string) Name {
	return Name{"file", fileHash, "info"}
}

func FileChunkName(fileHash string, chunkIdx int) Name {
	return Name{"file", fileHash, "chunk", fmt.Sprintf("%d", chunkIdx)}
}

func DriveName(nodeID string, pathComponents ...string) Name {
	n := Name{"drive", nodeID}
	return append(n, pathComponents...)
}

func PeerAnnounceName(nodeID string) Name {
	return Name{"discovery", "peers", nodeID}
}

func generateNonce() string {

	return fmt.Sprintf("%x", time.Now().UnixNano())
}
