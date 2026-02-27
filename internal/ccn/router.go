package ccn

/*
  CONCEPT: The CCN Router — the three tables working together
  ────────────────────────────────────────────────────────────
  When an Interest arrives, the router does this, in order:

  1. CHECK CONTENT STORE (CS)
     Do we already have this content cached?
     YES → send Data back immediately. Done.
     NO  → continue.

  2. CHECK PENDING INTEREST TABLE (PIT)
     Is there already an outstanding Interest for this exact name?
     YES → record this face in the existing PIT entry (aggregation).
            Don't forward again. Wait for the Data to arrive.
     NO  → continue.

  3. CHECK FORWARDING INFORMATION BASE (FIB)
     Which face(s) should we forward this Interest to?
     FIB maps name PREFIXES to faces (peer connections).
     MATCH → add to PIT, forward Interest to matching face(s).
     NO MATCH → send Nack (negative acknowledgement) — we can't route this.

  When Data arrives:
  1. Check PIT for matching Interest entries
  2. Store in Content Store (cache for future requests)
  3. Forward Data back to all faces that expressed Interest
  4. Remove PIT entry

  CONCEPT: Face
  ──────────────
  A "face" in CCN is like a network interface — it's an abstraction
  over any transport that can carry packets:
  - A WebSocket connection to a specific peer
  - A UDP multicast socket
  - A local application (chat, file manager, etc.)

  In our implementation, a Face is identified by a string ID.
  The router doesn't know or care what's behind the face ID.
  The caller is responsible for actually sending packets to faces.
*/

import (
	"log"
	"sync"
	"time"
)

// Face is an abstract network interface identified by a string.
// In practice: a peer's nodeID, "local" for local app, "multicast" for UDP.
type Face = string

const (
	FaceLocal     Face = "local"     // local application
	FaceMulticast Face = "multicast" // UDP multicast (all LAN peers)
)

// ── Forwarding Information Base (FIB) ────────────────────────────────────────

/*
  The FIB is the routing table.
  It maps name PREFIXES to faces.
  When forwarding an Interest, we find the LONGEST matching prefix.

  Example FIB:
  /chat         → [face:peer-AA, face:multicast]
  /drive/alice  → [face:peer-BB]
  /drive        → [face:multicast]
  /             → [face:multicast]   (default route)

  Interest for /drive/alice/thesis.pdf:
  Matches /drive/alice (length 2)  → peer-BB
  Matches /drive        (length 1)  → multicast
  Longest match wins: → peer-BB
*/

// fibEntry is one row in the FIB.
type fibEntry struct {
	prefix Name
	faces  map[Face]bool // set of faces for this prefix
}

// FIB is the Forwarding Information Base.
type FIB struct {
	mu      sync.RWMutex
	entries []*fibEntry // ordered; we scan for longest prefix match
}

func newFIB() *FIB {
	return &FIB{entries: make([]*fibEntry, 0, 32)}
}

// Add registers a prefix → face mapping in the FIB.
// If the prefix already exists, the face is added to its set.
func (f *FIB) Add(prefix Name, face Face) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, e := range f.entries {
		if e.prefix.Equal(prefix) {
			e.faces[face] = true
			return
		}
	}
	f.entries = append(f.entries, &fibEntry{
		prefix: prefix,
		faces:  map[Face]bool{face: true},
	})
}

// Remove removes a face from a prefix entry.
// If no faces remain for that prefix, the entry is deleted.
func (f *FIB) Remove(prefix Name, face Face) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i, e := range f.entries {
		if e.prefix.Equal(prefix) {
			delete(e.faces, face)
			if len(e.faces) == 0 {
				f.entries = append(f.entries[:i], f.entries[i+1:]...)
			}
			return
		}
	}
}

// Lookup finds the faces for the longest matching prefix of name.
// Returns nil if no match (not even a default route).
func (f *FIB) Lookup(name Name) []Face {
	f.mu.RLock()
	defer f.mu.RUnlock()

	var bestEntry *fibEntry
	bestLen := -1

	for _, e := range f.entries {
		if name.HasPrefix(e.prefix) && len(e.prefix) > bestLen {
			bestLen = len(e.prefix)
			bestEntry = e
		}
	}

	if bestEntry == nil {
		return nil
	}

	faces := make([]Face, 0, len(bestEntry.faces))
	for face := range bestEntry.faces {
		faces = append(faces, face)
	}
	return faces
}

// ── Pending Interest Table (PIT) ─────────────────────────────────────────────

/*
  The PIT tracks Interests we've forwarded but haven't received Data for yet.
  It serves two purposes:
  1. AGGREGATION: if multiple faces ask for the same name, we only forward
     the Interest ONCE and remember all the faces that want the data.
  2. LOOP PREVENTION: if an Interest comes back to us (routing loop),
     we've already forwarded it, so we drop the duplicate.

  Each PIT entry expires after the Interest's lifetime.
  Expired entries are cleaned up by a background ticker.

  Example:
  Interest arrives from peer-AA for /chat/room1/messages/42
  → No CS hit, no existing PIT entry
  → Create PIT entry: {name: ..., faces: [peer-AA], expires: now+4s}
  → Forward to faces from FIB

  While waiting, Interest arrives from peer-BB for SAME name
  → No CS hit
  → PIT entry EXISTS → just add peer-BB to faces: [peer-AA, peer-BB]
  → Don't forward again (already in flight)

  Data arrives for /chat/room1/messages/42
  → Store in CS
  → Look up PIT entry → faces: [peer-AA, peer-BB]
  → Send Data to peer-AA AND peer-BB
  → Delete PIT entry
*/

// pitEntry is one row in the PIT.
type pitEntry struct {
	name      Name
	nonces    map[string]bool // deduplication: nonce → seen
	faces     map[Face]bool   // who wants this data
	expiresAt time.Time
}

// PIT is the Pending Interest Table.
type PIT struct {
	mu      sync.Mutex
	entries map[string]*pitEntry // name.Key() → entry
}

func newPIT() *PIT {
	return &PIT{entries: make(map[string]*pitEntry)}
}

// RecordInterest adds an Interest to the PIT.
// Returns:
//   true  = new Interest, should be forwarded
//   false = duplicate or aggregated, do NOT forward again
func (p *PIT) RecordInterest(interest *Interest, inFace Face) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := interest.Name.Key()
	entry, exists := p.entries[key]

	if exists {
		// Check if this nonce is a duplicate (same Interest looped back)
		if entry.nonces[interest.Nonce] {
			return false // exact duplicate — drop
		}
		// Same name, different nonce — aggregation
		// Add the requesting face, but don't forward again
		entry.nonces[interest.Nonce] = true
		entry.faces[inFace] = true
		return false // aggregated — don't forward
	}

	// New Interest — create PIT entry and signal to forward
	lifetime := time.Duration(interest.InterestLifetimeMs) * time.Millisecond
	if lifetime <= 0 {
		lifetime = 4 * time.Second
	}

	p.entries[key] = &pitEntry{
		name:      interest.Name,
		nonces:    map[string]bool{interest.Nonce: true},
		faces:     map[Face]bool{inFace: true},
		expiresAt: time.Now().Add(lifetime),
	}
	return true // new — should forward
}

// ConsumePIT removes the PIT entry for name and returns all requesting faces.
// Called when Data arrives — we need to know who to send it to.
// Returns nil if no PIT entry exists (unsolicited data).
func (p *PIT) ConsumePIT(name Name) []Face {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := name.Key()
	entry, ok := p.entries[key]
	if !ok {
		return nil
	}

	delete(p.entries, key)

	faces := make([]Face, 0, len(entry.faces))
	for face := range entry.faces {
		faces = append(faces, face)
	}
	return faces
}

// cleanup removes expired PIT entries.
func (p *PIT) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	for key, entry := range p.entries {
		if now.After(entry.expiresAt) {
			delete(p.entries, key)
		}
	}
}

// ── Router ────────────────────────────────────────────────────────────────────

/*
  The Router ties together CS, PIT, and FIB.
  It processes incoming packets and calls callbacks to actually
  send packets out (the router doesn't know about WebSocket or UDP —
  that's the transport layer's job).

  CONCEPT: Callback-based design
  ────────────────────────────────
  The router needs to send packets out but doesn't know HOW.
  Instead of importing transport packages (circular import!),
  we use callbacks — the caller provides functions for sending.

  This is Dependency Inversion:
    High-level module (router) doesn't depend on low-level module (transport).
    Both depend on abstractions (callback function types).

  Router: "I need to send an Interest to face X"
  Caller provides: func(face Face, packet *Packet) { ws.SendToNode(face, packet) }
*/

// SendFunc is the callback for sending packets to a face.
// The router calls this; the caller wires it to the actual transport.
type SendFunc func(face Face, pkt *Packet)

// Router is the CCN forwarding engine.
type Router struct {
	cs      *ContentStore
	pit     *PIT
	fib     *FIB
	send    SendFunc
	nodeID  string
	stopCh  chan struct{}
}

// NewRouter creates a Router.
// send: callback that actually delivers packets to faces
// nodeID: this node's identity (used for loop detection)
// cacheMaxBytes: Content Store size limit
func NewRouter(nodeID string, cacheMaxBytes int64, send SendFunc) *Router {
	r := &Router{
		cs:     NewContentStore(cacheMaxBytes),
		pit:    newPIT(),
		fib:    newFIB(),
		send:   send,
		nodeID: nodeID,
		stopCh: make(chan struct{}),
	}
	go r.maintenance()
	return r
}

// Stop shuts down the router's background goroutines.
func (r *Router) Stop() {
	close(r.stopCh)
}

// ReceiveInterest processes an incoming Interest packet.
// inFace: where this Interest came from (peer nodeID, "local", "multicast")
func (r *Router) ReceiveInterest(interest *Interest, inFace Face) {
	// Loop detection — don't process Interests that have already visited us
	if interest.HasLooped(r.nodeID) {
		log.Printf("[ccn] dropped looping interest %s", interest.Name)
		return
	}

	// ── Step 1: Check Content Store ──────────────────────────────────────────
	var data *Data
	if interest.CanBePrefix {
		results := r.cs.GetByPrefix(interest.Name, interest.MustBeFresh)
		if len(results) > 0 {
			data = results[0] // return first match for prefix interests
		}
	} else {
		data = r.cs.Get(interest.Name, interest.MustBeFresh)
	}

	if data != nil {
		// Cache hit — send Data back to requester immediately
		log.Printf("[ccn] CS hit for %s → face %s", interest.Name, inFace)
		r.send(inFace, WrapData(data))
		return
	}

	// ── Step 2: Check/Update PIT ──────────────────────────────────────────────
	shouldForward := r.pit.RecordInterest(interest, inFace)
	if !shouldForward {
		// Already pending or duplicate — aggregated, don't forward again
		log.Printf("[ccn] PIT aggregated %s from face %s", interest.Name, inFace)
		return
	}

	// ── Step 3: Forward via FIB ───────────────────────────────────────────────
	faces := r.fib.Lookup(interest.Name)
	if len(faces) == 0 {
		// No route — send Nack by sending an error data packet back
		log.Printf("[ccn] no FIB route for %s", interest.Name)
		nack := NewData(interest.Name, []byte(`{"error":"no route"}`), ContentTypeJSON)
		r.send(inFace, WrapData(nack))
		return
	}

	// Add this node to the hop list before forwarding
	interest.AddHop(r.nodeID)

	// Forward to all matching faces (except where it came from)
	for _, face := range faces {
		if face != inFace { // don't send back the way it came
			log.Printf("[ccn] forwarding interest %s to face %s", interest.Name, face)
			r.send(face, WrapInterest(interest))
		}
	}
}

// ReceiveData processes an incoming Data packet.
// inFace: where this Data came from
func (r *Router) ReceiveData(data *Data, inFace Face) {
	// ── Step 1: Verify signature ──────────────────────────────────────────────
	/*
	  We verify before caching. A bad actor could send forged data.
	  If we cache it without verifying, we'd serve corrupt data to
	  everyone who asks — amplifying the attack.
	  Verify first, cache only if valid.
	*/
	if err := data.Verify(); err != nil {
		log.Printf("[ccn] invalid signature on data from face %s: %v", inFace, err)
		return // drop silently — don't reward the attacker with a response
	}

	// ── Step 2: Store in Content Store ───────────────────────────────────────
	r.cs.Put(data)
	log.Printf("[ccn] cached %s (%d bytes)", data.Name, len(data.Content))

	// ── Step 3: Look up PIT and forward to requesting faces ──────────────────
	faces := r.pit.ConsumePIT(data.Name)
	if faces == nil {
		// Unsolicited data — we cached it but no one asked for it.
		// This can happen with prefix interests or proactive pushes.
		log.Printf("[ccn] unsolicited data %s (cached, no PIT entry)", data.Name)
		return
	}

	// Send data to everyone who expressed interest
	for _, face := range faces {
		if face != inFace { // don't echo back
			log.Printf("[ccn] delivering %s to face %s", data.Name, face)
			r.send(face, WrapData(data))
		}
	}
}

// ProduceData is called by LOCAL applications to publish content.
// It puts the data directly into the CS and satisfies any pending interests.
// Use this when chat sends a message, file system serves a chunk, etc.
func (r *Router) ProduceData(data *Data) {
	r.ReceiveData(data, FaceLocal)
}

// ExpressInterest is called by LOCAL applications to request content.
// Returns immediately — data will arrive via the send callback to FaceLocal.
func (r *Router) ExpressInterest(interest *Interest) {
	r.ReceiveInterest(interest, FaceLocal)
}

// AddRoute registers a FIB entry: forward interests matching prefix to face.
func (r *Router) AddRoute(prefix Name, face Face) {
	r.fib.Add(prefix, face)
	log.Printf("[ccn] FIB: added route %s → %s", prefix, face)
}

// RemoveRoute removes a FIB entry.
func (r *Router) RemoveRoute(prefix Name, face Face) {
	r.fib.Remove(prefix, face)
	log.Printf("[ccn] FIB: removed route %s → %s", prefix, face)
}

// ContentStore exposes the content store for direct access (e.g. metrics).
func (r *Router) ContentStoreStats() ContentStoreStats {
	return r.cs.Stats()
}

// maintenance runs background cleanup tasks.
func (r *Router) maintenance() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.pit.cleanup()
		case <-r.stopCh:
			return
		}
	}
}