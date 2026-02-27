package transport

import "encoding/json"

type MessageType string

const (
	TypeJoin      MessageType = "join"
	TypeLeave     MessageType = "leave"
	TypeHello     MessageType = "hello"
	TypeOffer     MessageType = "offer"
	TypeAnswer    MessageType = "answer"
	TypeICE       MessageType = "ice"
	TypeChatMsg   MessageType = "chat_msg"
	TypePing      MessageType = "ping"
	TypePeerJoin  MessageType = "peer_join"
	TypePeerLeft  MessageType = "peer_left"
	TypePeerList  MessageType = "peer_list"
	TypePong      MessageType = "pong"
	TypeError     MessageType = "error"
	TypeRoomState MessageType = "room_state"
)

type Message struct {
	Type    MessageType     `json:"type"`
	Room    string          `json:"room,omitempty"`
	From    string          `json:"from,omitempty"`
	To      string          `json:"to,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   string          `json:"error,omitempty"`
}

type HelloPayload struct {
	DisplayName string   `json:"display_name"`
	NodeID      string   `json:"node_id"`
	Services    []string `json:"services"`
}

type PeerInfo struct {
	NodeID      string   `json:"node_id"`
	DisplayName string   `json:"display_name"`
	Services    []string `json:"services"`
}

type PeerListPayload struct {
	Peers []PeerInfo `json:"peers"`
}

type PeerJoinPayload struct {
	Peer PeerInfo `json:"peer"`
}

type PeerLeftPayload struct {
	NodeID string `json:"node_id"`
}

type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func NewErrorMsg(code, msg string) *Message {
	payload, _ := json.Marshal(ErrorPayload{Code: code, Message: msg})
	return &Message{Type: TypeError, Payload: payload, Error: msg}
}

func NewPeerJoinMsg(room string, peer PeerInfo) *Message {
	payload, _ := json.Marshal(PeerJoinPayload{Peer: peer})
	return &Message{Type: TypePeerJoin, Room: room, Payload: payload}
}

func NewPeerLeftMsg(room, nodeID string) *Message {
	payload, _ := json.Marshal(PeerLeftPayload{NodeID: nodeID})
	return &Message{Type: TypePeerLeft, Room: room, Payload: payload}
}

func NewPeerListMsg(room string, peers []PeerInfo) *Message {
	payload, _ := json.Marshal(PeerListPayload{Peers: peers})
	return &Message{Type: TypePeerList, Room: room, Payload: payload}
}

func NewPongMsg() *Message {
	return &Message{Type: TypePong}
}