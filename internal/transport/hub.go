package transport

/*
  CONCEPT: The Hub Pattern — why one goroutine owns all state
  ─────────────────────────────────────────────────────────────
  We have potentially hundreds of clients. Each has its own goroutine.
  They all need to access shared state: "who is in room X?", "add this client".

  Naive approach: use a mutex (sync.Mutex) to lock the state map.
    mu.Lock()
    rooms[roomID] = append(rooms[roomID], client)
    mu.Unlock()

  Problems with mutexes:
  - Easy to forget to unlock (deadlock)
  - Lock contention under high load
  - Hard to reason about what's protected

  Go's answer: "Don't communicate by sharing memory.
                Share memory by communicating."

  Hub pattern: ONE goroutine owns ALL shared state.
  ALL other goroutines communicate with it via CHANNELS.
  No mutex needed because only one goroutine ever touches the maps.

  ┌──────────────────────────────────────────────────────────────┐
  │                         Hub Goroutine                         │
  │                  (the only one touching maps)                 │
  │                                                               │
  │   rooms map        clients map                                │
  │   ─────────────    ───────────                                │
  │   room → []*Client id → *Client                              │
  └──────────┬────────────────────────────────────────────────────┘
             ▲ channels (thread-safe message passing)
  ┌──────────┴────────────┐
  │  register   chan *Client        ← new connection
  │  unregister chan *Client        ← disconnection
  │  incoming   chan *incomingMsg   ← message from any client
  └─────────────────────────────────────────────────────────────┘

  Every client goroutine sends to these channels.
  The hub goroutine reads from them in a select loop.
  State is ONLY ever modified inside that select loop.
  Zero race conditions. Zero mutexes. Completely predictable.
*/

import (
	"encoding/json"
	"log"
	"strings"
	"unicode/utf8"
)

// incomingMsg bundles a message with its sender.
// This is what client.readPump sends to hub.incoming.
type incomingMsg struct {
	client *Client
	msg    *Message
}

// Hub manages all connected clients and routes messages between them.
// Run it with: go hub.Run()
type Hub struct {
	// channels — the ONLY way to communicate with the hub goroutine
	register   chan *Client
	unregister chan *Client
	incoming   chan *incomingMsg

	// state — ONLY touched inside Run()'s select loop
	// clients: all connected clients indexed by nodeID
	clients map[string]*Client
	// rooms: roomID → set of clients in that room
	// We use map[*Client]bool as a set — Go has no built-in set type.
	// map[*Client]bool where value is always true is idiomatic Go set.
	rooms map[string]map[*Client]bool
}

// NewHub creates a Hub. Call hub.Run() in a goroutine to start it.
func NewHub() *Hub {
	return &Hub{
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
		incoming:   make(chan *incomingMsg, 512),
		clients:    make(map[string]*Client),
		rooms:      make(map[string]map[*Client]bool),
	}
}

// Run starts the hub's event loop. Must be called in its own goroutine:
//   go hub.Run()
//
// This function runs forever (until the process exits).
// It is the ONLY place that reads/writes hub.clients and hub.rooms.
func (h *Hub) Run() {
	log.Println("[hub] started")
	for {
		/*
		  CONCEPT: The central select loop
		  ──────────────────────────────────
		  This select handles three types of events:
		  1. A new client connects (register)
		  2. A client disconnects (unregister)
		  3. A client sends a message (incoming)

		  Go processes exactly ONE case per iteration.
		  Events queue up in the channels while hub is processing another event.
		  This serializes all state mutations — safe by construction.
		*/
		select {

		case client := <-h.register:
			h.handleRegister(client)

		case client := <-h.unregister:
			h.handleUnregister(client)

		case im := <-h.incoming:
			h.handleMessage(im.client, im.msg)
		}
	}
}

// ── Event handlers ────────────────────────────────────────────────────────────
// These are called from inside Run()'s select loop.
// They can safely read/write hub.clients and hub.rooms.

func (h *Hub) handleRegister(c *Client) {
	// A client just connected and upgraded to WebSocket.
	// They haven't identified themselves yet (no hello message).
	// We give them a temporary placeholder ID.
	// Real identity is set when they send TypeHello.
	tempID := "pending-" + c.conn.RemoteAddr().String()
	c.nodeID = tempID
	h.clients[tempID] = c
	log.Printf("[hub] registered: %s (pending hello)", c.conn.RemoteAddr())
}

func (h *Hub) handleUnregister(c *Client) {
	// Client disconnected — clean up everywhere.

	// 1. Remove from any room they were in
	if c.currentRoom != "" {
		h.leaveRoom(c)
	}

	// 2. Remove from global clients map
	delete(h.clients, c.nodeID)

	// 3. Close their send channel — signals writePump to exit
	//    IMPORTANT: only close a channel from the SENDER side.
	//    Hub is the sender (writes to c.send), so hub closes it.
	//    writePump is the receiver — it will see the close and exit.
	close(c.send)

	log.Printf("[hub] unregistered: %s", c.identity())
}

func (h *Hub) handleMessage(c *Client, msg *Message) {
	// c is nil for internal notifications (e.g. from discovery).
	// These are broadcast-only messages — no sender to reply to.
	if c == nil {
		switch msg.Type {
		case TypePeerJoin, TypePeerLeft:
			// Broadcast to all connected clients
			for _, client := range h.clients {
				client.sendMsg(msg)
			}
		}
		return
	}

	// Route based on message type
	switch msg.Type {

	case TypeHello:
		h.handleHello(c, msg)

	case TypeJoin:
		h.handleJoin(c, msg)

	case TypeLeave:
		h.leaveRoom(c)

	case TypePing:
		c.sendMsg(NewPongMsg())

	// WebRTC signaling — server relays these without reading the payload
	case TypeOffer, TypeAnswer, TypeICE:
		h.handleSignaling(c, msg)

	// Chat message — broadcast to room or direct to peer
	case TypeChatMsg:
		h.handleChat(c, msg)

	default:
		c.sendMsg(NewErrorMsg("unknown_type", "unknown message type: "+string(msg.Type)))
	}
}

// ── Message-specific handlers ─────────────────────────────────────────────────

func (h *Hub) handleHello(c *Client, msg *Message) {
	// Parse the hello payload to get client's identity
	var hello HelloPayload
	if err := json.Unmarshal(msg.Payload, &hello); err != nil {
		c.sendMsg(NewErrorMsg("bad_hello", "hello payload must have display_name, node_id, services"))
		return
	}

	// Validate display_name
	hello.DisplayName = strings.TrimSpace(hello.DisplayName)
	if hello.DisplayName == "" || utf8.RuneCountInString(hello.DisplayName) > 64 {
		c.sendMsg(NewErrorMsg("bad_hello", "display_name must be 1-64 characters"))
		return
	}

	// Validate node_id — must be non-empty
	hello.NodeID = strings.TrimSpace(hello.NodeID)
	if hello.NodeID == "" {
		c.sendMsg(NewErrorMsg("bad_hello", "node_id is required"))
		return
	}

	// Re-key the client in our map (was "pending-addr", now real nodeID)
	// This is why we use nodeID as the map key — it changes after hello.
	delete(h.clients, c.nodeID)
	c.nodeID = hello.NodeID
	c.displayName = hello.DisplayName
	c.services = hello.Services
	h.clients[c.nodeID] = c

	log.Printf("[hub] hello from %s services=%v", c.identity(), c.services)
}

func (h *Hub) handleJoin(c *Client, msg *Message) {
	// Validate room name
	room := strings.TrimSpace(msg.Room)
	if room == "" {
		c.sendMsg(NewErrorMsg("bad_room", "room name cannot be empty"))
		return
	}
	if len(room) > 128 {
		c.sendMsg(NewErrorMsg("bad_room", "room name must be 128 characters or fewer"))
		return
	}
	// Only allow alphanumeric, hyphen, underscore — prevent path traversal
	// and other injection attacks via room names
	if !isValidRoomName(room) {
		c.sendMsg(NewErrorMsg("bad_room", "room name may only contain letters, numbers, hyphens, and underscores"))
		return
	}

	// Leave current room if in one
	if c.currentRoom != "" {
		h.leaveRoom(c)
	}

	// Create room if it doesn't exist
	if h.rooms[room] == nil {
		h.rooms[room] = make(map[*Client]bool)
	}

	// Add client to room
	h.rooms[room][c] = true
	c.currentRoom = room

	// Build peer list of everyone currently in the room (excluding the newcomer)
	var peers []PeerInfo
	for peer := range h.rooms[room] {
		if peer != c {
			peers = append(peers, peer.asPeerInfo())
		}
	}
	if peers == nil {
		peers = []PeerInfo{} // never send null, always send empty array
	}

	// Tell the newcomer who's already here
	c.sendMsg(NewPeerListMsg(room, peers))

	// Tell everyone else that a new peer joined
	joinMsg := NewPeerJoinMsg(room, c.asPeerInfo())
	h.broadcastRoom(room, joinMsg, c) // exclude the newcomer (they know they joined)

	log.Printf("[hub] %s joined room %q (%d peers)", c.identity(), room, len(h.rooms[room]))
}

func (h *Hub) leaveRoom(c *Client) {
	room := c.currentRoom
	if room == "" {
		return
	}

	// Remove from room
	delete(h.rooms[room], c)
	c.currentRoom = ""

	// Clean up empty rooms — don't let the map grow forever
	if len(h.rooms[room]) == 0 {
		delete(h.rooms, room)
		log.Printf("[hub] room %q deleted (empty)", room)
	} else {
		// Tell remaining peers this client left
		leftMsg := NewPeerLeftMsg(room, c.nodeID)
		h.broadcastRoom(room, leftMsg, nil) // nil = send to everyone
		log.Printf("[hub] %s left room %q (%d peers remain)", c.identity(), room, len(h.rooms[room]))
	}
}

func (h *Hub) handleSignaling(c *Client, msg *Message) {
	/*
	  CONCEPT: WebRTC Signaling Relay
	  ──────────────────────────────────
	  The server's job for WebRTC is minimal:
	  - Client A sends offer → server forwards to everyone in the room
	  - Client B sends answer → server forwards to everyone in the room
	  - Both send ICE candidates → server forwards to everyone in the room

	  The server NEVER reads the SDP or ICE payload. It's opaque.
	  The server is just a mailman — deliver to the right room.

	  DIRECTED messages (msg.To is set):
	  Forward only to the specific target peer.
	  Used in multi-party calls where you want to offer to a specific peer.

	  BROADCAST messages (msg.To is empty):
	  Forward to everyone in the room except the sender.
	  Simpler but only works for 1-to-1 rooms.
	*/
	if c.currentRoom == "" {
		c.sendMsg(NewErrorMsg("not_in_room", "join a room before sending signaling messages"))
		return
	}

	if msg.To != "" {
		// Directed: send only to the target peer
		target := h.clients[msg.To]
		if target == nil {
			c.sendMsg(NewErrorMsg("peer_not_found", "target peer is not connected"))
			return
		}
		// Make sure target is in the same room — prevent cross-room snooping
		if target.currentRoom != c.currentRoom {
			c.sendMsg(NewErrorMsg("peer_not_in_room", "target peer is not in your room"))
			return
		}
		target.sendMsg(msg)
	} else {
		// Broadcast to room
		h.broadcastRoom(c.currentRoom, msg, c)
	}
}

func (h *Hub) handleChat(c *Client, msg *Message) {
	if c.currentRoom == "" {
		c.sendMsg(NewErrorMsg("not_in_room", "join a room before sending messages"))
		return
	}

	// Validate chat payload size — prevent huge messages
	if len(msg.Payload) > 4*1024 {
		c.sendMsg(NewErrorMsg("message_too_large", "chat message must be under 4KB"))
		return
	}

	// Stamp the room and relay — server doesn't read the message content
	msg.Room = c.currentRoom
	h.broadcastRoom(c.currentRoom, msg, nil) // nil = send to everyone including sender
}

// ── Broadcast helpers ─────────────────────────────────────────────────────────

// broadcastRoom sends a message to all clients in a room.
// If exclude is non-nil, that client is skipped (used for join notifications
// where we don't echo back to the sender).
func (h *Hub) broadcastRoom(room string, msg *Message, exclude *Client) {
	peers, ok := h.rooms[room]
	if !ok {
		return
	}
	for client := range peers {
		if client != exclude {
			client.sendMsg(msg)
		}
	}
}

// Stats returns a snapshot of hub state for the health/metrics endpoint.
// Safe to call from outside the hub goroutine? NO — but for a metrics
// endpoint, a slightly stale count is acceptable.
// For strict correctness we'd send a request through a channel.
// That's over-engineering for a debug endpoint.
func (h *Hub) Stats() (clients, rooms int) {
	return len(h.clients), len(h.rooms)
}

// isValidRoomName allows only: a-z A-Z 0-9 - _
// Rejects anything that could be used for injection or path traversal.
func isValidRoomName(name string) bool {
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// ── External notifications ────────────────────────────────────────────────────
// Called by discovery when a peer appears/disappears on the LAN.
// These broadcast to all WebSocket clients so the frontend updates instantly.

// NotifyPeerJoined broadcasts a peer_join event to all connected clients.
// Called by discovery when a new LAN peer is found via UDP multicast.
func (h *Hub) NotifyPeerJoined(nodeID, displayName string, services []string) {
	msg := NewPeerJoinMsg("__lan__", PeerInfo{
		NodeID:      nodeID,
		DisplayName: displayName,
		Services:    services,
	})
	// Send through the incoming channel so the hub goroutine handles it safely.
	// We cannot touch h.clients directly from outside the Run() goroutine.
	select {
	case h.incoming <- &incomingMsg{client: nil, msg: msg}:
	default:
		log.Printf("[hub] notify channel full, dropping peer_join for %s", nodeID[:8])
	}
}

// NotifyPeerLeft broadcasts a peer_left event to all connected clients.
// Called by discovery when a LAN peer times out.
func (h *Hub) NotifyPeerLeft(nodeID string) {
	msg := NewPeerLeftMsg("__lan__", nodeID)
	select {
	case h.incoming <- &incomingMsg{client: nil, msg: msg}:
	default:
		log.Printf("[hub] notify channel full, dropping peer_left for %s", nodeID[:8])
	}
}