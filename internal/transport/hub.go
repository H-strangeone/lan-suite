package transport

import (
	"encoding/json"
	"log"
	"strings"
	"time"
	"unicode/utf8"
)

// ── Interfaces ────────────────────────────────────────────────────────────────

// ChatHandler is the interface the hub uses to process chat messages.
// Defined here so chat can implement it without an import cycle.
type ChatHandler interface {
	HandleMessage(nodeID, displayName, roomID string, payload []byte) error
}

// RoomRegistry is the interface the hub uses for room auth + creation.
// Implemented by chat.Manager — defined here to avoid import cycle.
type RoomRegistry interface {
	// CreateRoom creates a room and returns its ID, or an error.
	CreateRoom(name, creatorID, creatorName, password string) (roomID string, err error)
	// CheckRoomPassword returns nil if password is correct (or room is public).
	CheckRoomPassword(roomID, password string) error
	// DeleteRoom permanently removes a room from the registry (used on disband).
	DeleteRoom(roomID string)
	// RoomExists returns true if the room ID is known to the registry.
	RoomExists(roomID string) bool
	// RoomListJSON returns all rooms as JSON bytes for broadcasting.
	// member counts come from hub.RoomMemberCounts() passed in.
	RoomListJSON(memberCounts map[string]int) ([]byte, error)
}

// ── Hub ───────────────────────────────────────────────────────────────────────

type incomingMsg struct {
	client *Client
	msg    *Message
}

// Hub manages all connected clients and routes messages.
// ONE goroutine (Run) owns all maps. Everything else sends via channels.
type Hub struct {
	register   chan *Client
	unregister chan *Client
	incoming   chan *incomingMsg

	clients map[string]*Client          // nodeID → client
	rooms   map[string]map[*Client]bool // roomID → members

	chatHandler ChatHandler
	roomReg     RoomRegistry
}

func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		incoming:   make(chan *incomingMsg, 512),
		clients:    make(map[string]*Client),
		rooms:      make(map[string]map[*Client]bool),
	}
}

func (h *Hub) SetChatHandler(ch ChatHandler) { h.chatHandler = ch }
func (h *Hub) SetRoomRegistry(rr RoomRegistry) { h.roomReg = rr }

// Run is the hub's single-threaded event loop.
func (h *Hub) Run() {
	log.Println("[hub] started")
	for {
		select {
		case c := <-h.register:
			h.handleRegister(c)
		case c := <-h.unregister:
			h.handleUnregister(c)
		case im := <-h.incoming:
			h.handleMessage(im.client, im.msg)
		}
	}
}

// ── Registration ──────────────────────────────────────────────────────────────

func (h *Hub) handleRegister(c *Client) {
	tempID := "pending-" + c.conn.RemoteAddr().String()
	c.nodeID = tempID
	h.clients[tempID] = c
	log.Printf("[hub] registered: %s (pending hello)", c.conn.RemoteAddr())
}

func (h *Hub) handleUnregister(c *Client) {
	if c.currentRoom != "" {
		h.leaveRoom(c)
	}
	if h.clients[c.nodeID] == c {
		delete(h.clients, c.nodeID)
	}
	safeClose(c.send)
	log.Printf("[hub] unregistered: %s", c.logID())
}

// ── Message dispatch ──────────────────────────────────────────────────────────

func (h *Hub) handleMessage(c *Client, msg *Message) {
	if c == nil {
		// Internal notification from discovery or chat manager
		switch msg.Type {
		case TypePeerJoin, TypePeerLeft:
			for _, cl := range h.clients {
				// __lan__ discovery events fire every 5 s from the discovery loop.
				// Clients already inside a real room have authoritative peer state
				// from room-scoped events (peer_list / peer_join / peer_left).
				// Sending __lan__ to them is redundant noise that causes the frontend
				// peer list to grow on every tick even with dedup logic in place.
				if msg.Room == "__lan__" && cl.currentRoom != "" {
					continue
				}
				cl.sendMsg(msg)
			}
		case TypeChatMsg:
			h.broadcastRoom(msg.Room, msg, nil)
		case TypeRoomList:
			// Broadcast updated room list to all clients
			for _, cl := range h.clients {
				cl.sendMsg(msg)
			}
		}
		return
	}

	switch msg.Type {
	case TypeHello:
		h.handleHello(c, msg)
	case TypeCreateRoom:
		h.handleCreateRoom(c, msg)
	case TypeJoin:
		h.handleJoin(c, msg) // join by name (default/public rooms)
	case TypeJoinById:
		h.handleJoinByID(c, msg) // join by UUID with optional password
	case TypeRoomListReq:
		h.handleRoomListReq(c)
	case TypeLeave:
		h.leaveRoom(c)
		c.sendMsg(&Message{Type: TypePeerList, Payload: []byte(`{"peers":[]}`)}) // confirm left
	case TypePing:
		c.sendMsg(NewPongMsg())
	case TypeOffer, TypeAnswer, TypeICE, TypeCallHangup, TypeCallReject:
		h.handleSignaling(c, msg)
	case TypeKickMember:
		h.handleKick(c, msg)
	case TypeDisbandRoom:
		h.handleDisband(c, msg)
	case TypeChatMsg:
		h.handleChat(c, msg)
	default:
		c.sendMsg(NewErrorMsg("unknown_type", "unknown message type: "+string(msg.Type)))
	}
}

// ── Hello ─────────────────────────────────────────────────────────────────────

func (h *Hub) handleHello(c *Client, msg *Message) {
	var hello HelloPayload
	if err := json.Unmarshal(msg.Payload, &hello); err != nil {
		c.sendMsg(NewErrorMsg("bad_hello", "hello payload must have display_name, node_id, services"))
		return
	}

	hello.DisplayName = strings.TrimSpace(hello.DisplayName)
	hello.NodeID = strings.TrimSpace(hello.NodeID)

	if hello.DisplayName == "" || utf8.RuneCountInString(hello.DisplayName) > 64 {
		c.sendMsg(NewErrorMsg("bad_hello", "display_name must be 1-64 characters"))
		return
	}
	if hello.NodeID == "" {
		c.sendMsg(NewErrorMsg("bad_hello", "node_id is required"))
		return
	}

	// SECURITY: hello.NodeID must match JWT claim — client cannot forge identity
	if hello.NodeID != c.identity.NodeID {
		c.sendMsg(NewErrorMsg("bad_hello", "node_id must match your token"))
		return
	}

	// Re-key: delete pending entry, insert real nodeID
	delete(h.clients, c.nodeID)
	if existing, ok := h.clients[hello.NodeID]; ok && existing != c {
		log.Printf("[hub] evicting duplicate connection for %s", safeID(hello.NodeID))
		if existing.currentRoom != "" {
			h.leaveRoom(existing)
		}
		safeClose(existing.send)
		delete(h.clients, hello.NodeID)
	}
	c.nodeID = hello.NodeID
	c.displayName = hello.DisplayName
	c.services = hello.Services
	h.clients[c.nodeID] = c

	log.Printf("[hub] hello from %s services=%v", c.logID(), c.services)
}

// ── Room creation ─────────────────────────────────────────────────────────────

func (h *Hub) handleCreateRoom(c *Client, msg *Message) {
	if h.roomReg == nil {
		c.sendMsg(NewErrorMsg("unavailable", "room registry not ready"))
		return
	}

	var req CreateRoomPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendMsg(NewErrorMsg("bad_payload", "expected {name, password?}"))
		return
	}
	req.Name = strings.TrimSpace(strings.ToLower(req.Name))
	if req.Name == "" || len(req.Name) > 64 || !isValidRoomName(req.Name) {
		c.sendMsg(NewErrorMsg("bad_room", "name must be lowercase a-z 0-9 - _ and max 64 chars"))
		return
	}

	roomID, err := h.roomReg.CreateRoom(req.Name, c.nodeID, c.displayName, req.Password)
	if err != nil {
		c.sendMsg(NewErrorMsg("create_failed", err.Error()))
		return
	}

	log.Printf("[hub] room created: %q (%s) by %s", req.Name, roomID, c.logID())

	// Broadcast updated room list to ALL clients immediately
	h.broadcastRoomList()

	// Auto-join creator into the new room
	h.joinRoomByID(c, roomID, "")
}

// ── Room list ─────────────────────────────────────────────────────────────────

func (h *Hub) handleRoomListReq(c *Client) {
	if h.roomReg == nil {
		return
	}
	data, err := h.roomReg.RoomListJSON(h.RoomMemberCounts())
	if err != nil {
		return
	}
	c.sendMsg(&Message{Type: TypeRoomList, Payload: data})
}

// broadcastRoomList sends the full room list to every connected client.
// Called after a room is created so all open tabs update instantly.
func (h *Hub) broadcastRoomList() {
	if h.roomReg == nil {
		return
	}
	data, err := h.roomReg.RoomListJSON(h.RoomMemberCounts())
	if err != nil {
		return
	}
	msg := &Message{Type: TypeRoomList, Payload: data}
	for _, cl := range h.clients {
		cl.sendMsg(msg)
	}
}

// ── Join by name (legacy/default rooms) ──────────────────────────────────────

func (h *Hub) handleJoin(c *Client, msg *Message) {
	room := strings.TrimSpace(msg.Room)
	if room == "" || len(room) > 128 || !isValidRoomName(room) {
		c.sendMsg(NewErrorMsg("bad_room", "invalid room name"))
		return
	}

	// For default rooms, name == ID. Check registry by the room value directly.
	if h.roomReg != nil && !h.roomReg.RoomExists(room) {
		c.sendMsg(NewErrorMsg("room_not_found", "room not found — use join_by_id for user-created rooms"))
		return
	}

	h.joinRoomByID(c, room, "")
}

// ── Join by UUID ──────────────────────────────────────────────────────────────

func (h *Hub) handleJoinByID(c *Client, msg *Message) {
	var req JoinByIDPayload
	if err := json.Unmarshal(msg.Payload, &req); err != nil {
		c.sendMsg(NewErrorMsg("bad_payload", "expected {room_id, password?}"))
		return
	}
	req.RoomID = strings.TrimSpace(req.RoomID)
	if req.RoomID == "" {
		c.sendMsg(NewErrorMsg("bad_room", "room_id required"))
		return
	}

	if h.roomReg != nil {
		if !h.roomReg.RoomExists(req.RoomID) {
			c.sendMsg(NewErrorMsg("room_not_found", "room not found"))
			return
		}
		if err := h.roomReg.CheckRoomPassword(req.RoomID, req.Password); err != nil {
			c.sendMsg(NewErrorMsg("wrong_password", err.Error()))
			return
		}
	}

	h.joinRoomByID(c, req.RoomID, req.Password)
}

// joinRoomByID does the actual room join — shared by handleJoin and handleJoinByID.
func (h *Hub) joinRoomByID(c *Client, roomID, _ string) {
	if c.currentRoom != "" {
		h.leaveRoom(c)
	}

	if h.rooms[roomID] == nil {
		h.rooms[roomID] = make(map[*Client]bool)
	}
	h.rooms[roomID][c] = true
	c.currentRoom = roomID

	var peers []PeerInfo
	for peer := range h.rooms[roomID] {
		if peer != c {
			peers = append(peers, peer.asPeerInfo())
		}
	}
	if peers == nil {
		peers = []PeerInfo{}
	}

	c.sendMsg(NewPeerListMsg(roomID, peers))
	h.broadcastRoom(roomID, NewPeerJoinMsg(roomID, c.asPeerInfo()), c)

	log.Printf("[hub] %s joined room %q (%d peers)", c.logID(), roomID, len(h.rooms[roomID]))
}

// ── Leave ─────────────────────────────────────────────────────────────────────

func (h *Hub) leaveRoom(c *Client) {
	room := c.currentRoom
	if room == "" {
		return
	}
	delete(h.rooms[room], c)
	c.currentRoom = ""

	if len(h.rooms[room]) == 0 {
		delete(h.rooms, room)
		log.Printf("[hub] room %q empty", room)
	} else {
		h.broadcastRoom(room, NewPeerLeftMsg(room, c.nodeID), nil)
		log.Printf("[hub] %s left room %q", c.logID(), room)
	}

	// Update room list counts after someone leaves
	go func() {
		select {
		case h.incoming <- &incomingMsg{client: nil, msg: &Message{Type: TypeRoomList}}:
		default:
		}
	}()
}

// ── Signaling ─────────────────────────────────────────────────────────────────
// Routes offer/answer/ice/call_hangup/call_reject to any connected peer by node_id.
// No room membership required — a call should work regardless of what screen
// the callee is on. msg.From is stamped here so the receiver always knows the caller.

func (h *Hub) handleSignaling(c *Client, msg *Message) {
	if msg.To == "" {
		c.sendMsg(NewErrorMsg("no_target", "signaling requires msg.to"))
		return
	}
	target := h.clients[msg.To]
	if target == nil {
		c.sendMsg(NewErrorMsg("peer_not_found", "target peer is not connected"))
		return
	}
	msg.From = c.nodeID // stamp sender identity
	target.sendMsg(msg)
}

// ── Kick member ───────────────────────────────────────────────────────────────
// Only the creator of a room (checked client-side via room meta) can kick.
// Hub enforces: kicker must be in the same room as the target.

func (h *Hub) handleKick(c *Client, msg *Message) {
	var req struct {
		NodeID string `json:"node_id"`
	}
	if err := json.Unmarshal(msg.Payload, &req); err != nil || req.NodeID == "" {
		c.sendMsg(NewErrorMsg("bad_payload", "kick requires {node_id}"))
		return
	}
	if c.currentRoom == "" {
		c.sendMsg(NewErrorMsg("not_in_room", "you are not in a room"))
		return
	}
	target := h.clients[req.NodeID]
	if target == nil {
		c.sendMsg(NewErrorMsg("peer_not_found", "target not connected"))
		return
	}
	if target.currentRoom != c.currentRoom {
		c.sendMsg(NewErrorMsg("peer_not_in_room", "target not in your room"))
		return
	}
	// Tell the kicked peer to leave
	target.sendMsg(&Message{Type: TypeKicked, Room: c.currentRoom})
	h.leaveRoom(target)
	log.Printf("[hub] %s kicked %s from %s", c.logID(), safeID(req.NodeID), c.currentRoom)
}

// ── Disband room ──────────────────────────────────────────────────────────────
// Notifies all members, forces them out, deletes the room from the hub.

func (h *Hub) handleDisband(c *Client, msg *Message) {
	room := c.currentRoom
	if room == "" {
		c.sendMsg(NewErrorMsg("not_in_room", "you are not in a room"))
		return
	}
	disbanded := &Message{Type: TypeRoomDisbanded, Room: room}
	// Collect members first — can't iterate while modifying
	members := make([]*Client, 0, len(h.rooms[room]))
	for cl := range h.rooms[room] {
		members = append(members, cl)
	}
	// Notify and remove
	for _, cl := range members {
		cl.sendMsg(disbanded)
		cl.currentRoom = ""
	}
	delete(h.rooms, room)
	// Remove from persistent registry so it never appears in room lists again
	if h.roomReg != nil {
		h.roomReg.DeleteRoom(room)
	}
	log.Printf("[hub] room %s disbanded by %s", room, c.logID())
	h.broadcastRoomList()
}

// ── Chat ──────────────────────────────────────────────────────────────────────

func (h *Hub) handleChat(c *Client, msg *Message) {
	if c.currentRoom == "" {
		c.sendMsg(NewErrorMsg("not_in_room", "join a room before sending messages"))
		return
	}
	if len(msg.Payload) > 4*1024 {
		c.sendMsg(NewErrorMsg("message_too_large", "chat message must be under 4KB"))
		return
	}
	msg.Room = c.currentRoom

	if h.chatHandler != nil {
		if err := h.chatHandler.HandleMessage(c.nodeID, c.displayName, c.currentRoom, msg.Payload); err != nil {
			c.sendMsg(NewErrorMsg("chat_error", err.Error()))
		}
		return
	}
	h.broadcastRoom(c.currentRoom, msg, nil)
}

// ── Broadcast helpers ─────────────────────────────────────────────────────────

func (h *Hub) broadcastRoom(room string, msg *Message, exclude *Client) {
	for cl := range h.rooms[room] {
		if cl != exclude {
			cl.sendMsg(msg)
		}
	}
}

// ── Stats ─────────────────────────────────────────────────────────────────────

func (h *Hub) Stats() (clients, rooms int) {
	return len(h.clients), len(h.rooms)
}

// RoomMemberCounts returns a map of roomID → current live member count.
// Called by the room registry when building room list payloads.
func (h *Hub) RoomMemberCounts() map[string]int {
	counts := make(map[string]int, len(h.rooms))
	for roomID, members := range h.rooms {
		counts[roomID] = len(members)
	}
	return counts
}

// ── Discovery notifications ───────────────────────────────────────────────────

func (h *Hub) NotifyPeerJoined(nodeID, displayName string, services []string) {
	msg := NewPeerJoinMsg("__lan__", PeerInfo{
		NodeID: nodeID, DisplayName: displayName, Services: services,
	})
	select {
	case h.incoming <- &incomingMsg{client: nil, msg: msg}:
	default:
		log.Printf("[hub] notify full, dropping peer_join for %s", safeID(nodeID))
	}
}

func (h *Hub) NotifyPeerLeft(nodeID string) {
	msg := NewPeerLeftMsg("__lan__", nodeID)
	select {
	case h.incoming <- &incomingMsg{client: nil, msg: msg}:
	default:
		log.Printf("[hub] notify full, dropping peer_left for %s", safeID(nodeID))
	}
}

func (h *Hub) BroadcastToRoom(roomID string, msg *Message) {
	select {
	case h.incoming <- &incomingMsg{client: nil, msg: msg}:
	default:
		log.Printf("[hub] broadcast full, dropping %s to %s", msg.Type, roomID)
	}
}

// ── Safe helpers ──────────────────────────────────────────────────────────────

func safeClose(ch chan *Message) {
	defer func() { recover() }()
	close(ch)
}

func safeID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func isValidRoomName(name string) bool {
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

var _ = time.Second