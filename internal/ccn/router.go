package ccn

import (
	"log"
	"sync"
	"time"
)

type Face = string

const (
	FaceLocal     Face = "local"
	FaceMulticast Face = "multicast"
)

type fibEntry struct {
	prefix Name
	faces  map[Face]bool
}

type FIB struct {
	mu      sync.RWMutex
	entries []*fibEntry
}

func newFIB() *FIB {
	return &FIB{entries: make([]*fibEntry, 0, 32)}
}

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

type pitEntry struct {
	name      Name
	nonces    map[string]bool
	faces     map[Face]bool
	expiresAt time.Time
}

type PIT struct {
	mu      sync.Mutex
	entries map[string]*pitEntry
}

func newPIT() *PIT {
	return &PIT{entries: make(map[string]*pitEntry)}
}

func (p *PIT) RecordInterest(interest *Interest, inFace Face) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	key := interest.Name.Key()
	entry, exists := p.entries[key]

	if exists {

		if entry.nonces[interest.Nonce] {
			return false
		}

		entry.nonces[interest.Nonce] = true
		entry.faces[inFace] = true
		return false
	}

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
	return true
}

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

type SendFunc func(face Face, pkt *Packet)

type Router struct {
	cs     *ContentStore
	pit    *PIT
	fib    *FIB
	send   SendFunc
	nodeID string
	stopCh chan struct{}
}

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

func (r *Router) Stop() {
	close(r.stopCh)
}

func (r *Router) ReceiveInterest(interest *Interest, inFace Face) {

	if interest.HasLooped(r.nodeID) {
		log.Printf("[ccn] dropped looping interest %s", interest.Name)
		return
	}

	var data *Data
	if interest.CanBePrefix {
		results := r.cs.GetByPrefix(interest.Name, interest.MustBeFresh)
		if len(results) > 0 {
			data = results[0]
		}
	} else {
		data = r.cs.Get(interest.Name, interest.MustBeFresh)
	}

	if data != nil {

		log.Printf("[ccn] CS hit for %s → face %s", interest.Name, inFace)
		r.send(inFace, WrapData(data))
		return
	}

	shouldForward := r.pit.RecordInterest(interest, inFace)
	if !shouldForward {

		log.Printf("[ccn] PIT aggregated %s from face %s", interest.Name, inFace)
		return
	}

	faces := r.fib.Lookup(interest.Name)
	if len(faces) == 0 {

		log.Printf("[ccn] no FIB route for %s", interest.Name)
		nack := NewData(interest.Name, []byte(`{"error":"no route"}`), ContentTypeJSON)
		r.send(inFace, WrapData(nack))
		return
	}

	interest.AddHop(r.nodeID)

	for _, face := range faces {
		if face != inFace {
			log.Printf("[ccn] forwarding interest %s to face %s", interest.Name, face)
			r.send(face, WrapInterest(interest))
		}
	}
}

func (r *Router) ReceiveData(data *Data, inFace Face) {

	if err := data.Verify(); err != nil {
		log.Printf("[ccn] invalid signature on data from face %s: %v", inFace, err)
		return
	}

	r.cs.Put(data)
	log.Printf("[ccn] cached %s (%d bytes)", data.Name, len(data.Content))

	faces := r.pit.ConsumePIT(data.Name)
	if faces == nil {

		log.Printf("[ccn] unsolicited data %s (cached, no PIT entry)", data.Name)
		return
	}

	for _, face := range faces {
		if face != inFace {
			log.Printf("[ccn] delivering %s to face %s", data.Name, face)
			r.send(face, WrapData(data))
		}
	}
}

func (r *Router) ProduceData(data *Data) {
	r.ReceiveData(data, FaceLocal)
}

func (r *Router) ExpressInterest(interest *Interest) {
	r.ReceiveInterest(interest, FaceLocal)
}

func (r *Router) AddRoute(prefix Name, face Face) {
	r.fib.Add(prefix, face)
	log.Printf("[ccn] FIB: added route %s → %s", prefix, face)
}

func (r *Router) RemoveRoute(prefix Name, face Face) {
	r.fib.Remove(prefix, face)
	log.Printf("[ccn] FIB: removed route %s → %s", prefix, face)
}

func (r *Router) ContentStoreStats() ContentStoreStats {
	return r.cs.Stats()
}

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
