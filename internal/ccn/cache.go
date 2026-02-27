package ccn

/*
  CONCEPT: Content Store (CS)
  ────────────────────────────
  The Content Store is a cache of Data packets this node has seen.
  It's the first thing checked when an Interest arrives:
  "Do I already have this content? Serve it immediately."

  WITHOUT a content store:
  Every Interest goes to the network, even if 10 nodes just asked
  for the same thing 2 seconds ago.

  WITH a content store:
  The first request fetches from the network.
  The next 9 requests are served instantly from local cache.
  This is CCN's killer feature — the network itself becomes a cache.

  CONCEPT: LRU (Least Recently Used) eviction
  ──────────────────────────────────────────────
  The cache can't grow forever — we have a max size in bytes.
  When the cache is full and a new item arrives, we need to evict something.
  LRU evicts the item that was accessed LEAST RECENTLY.
  Logic: "if you haven't been asked for in a while, you're probably not needed"

  How LRU works (doubly-linked list + map):
  ┌─────┐   ┌─────┐   ┌─────┐   ┌─────┐
  │ MRU │ ↔ │  B  │ ↔ │  C  │ ↔ │ LRU │
  └─────┘   └─────┘   └─────┘   └─────┘
   newest                         oldest → evicted first

  On access: move item to MRU end (just used, not going to evict)
  On insert: add to MRU end
  On eviction: remove from LRU end

  The map gives O(1) lookup. The list gives O(1) insert/remove.
  Combined: O(1) get, O(1) put, O(1) evict. Very efficient.

  We implement a simplified version: evict oldest-inserted when full.
  Full LRU with move-to-front adds complexity we don't need yet.
*/

import (
	"log"
	"sync"
	"time"
)

// cacheEntry wraps a Data packet with cache bookkeeping.
type cacheEntry struct {
	data      *Data
	sizeBytes int
	insertedAt time.Time
	lastAccess time.Time
	hits       int // how many times this entry has been served
}

// ContentStore is a thread-safe, size-bounded cache of Data packets.
// It is safe to use from multiple goroutines.
type ContentStore struct {
	/*
	  CONCEPT: sync.RWMutex
	  ──────────────────────
	  sync.Mutex: one goroutine at a time, reads AND writes block each other.
	  sync.RWMutex: MULTIPLE goroutines can READ simultaneously.
	                Only ONE goroutine can WRITE (and it blocks all readers).

	  Use RWMutex when reads are far more frequent than writes.
	  The Content Store is read-heavy: many interests served from cache,
	  few new data items inserted. RWMutex gives better throughput.

	  mu.RLock() / mu.RUnlock()  → for read operations (Get)
	  mu.Lock()  / mu.Unlock()   → for write operations (Put, evict)
	*/
	mu       sync.RWMutex
	entries  map[string]*cacheEntry // name.Key() → entry
	maxBytes int64                  // hard limit on total cache size
	curBytes int64                  // current total size
	// Ordered insertion list for eviction (oldest first)
	// key order maintained via insertOrder slice
	insertOrder []string
}

// NewContentStore creates a ContentStore with the given max size in bytes.
// maxBytes = 0 means unlimited (not recommended for production).
func NewContentStore(maxBytes int64) *ContentStore {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024 * 1024 // 256MB default
	}
	return &ContentStore{
		entries:     make(map[string]*cacheEntry),
		maxBytes:    maxBytes,
		insertOrder: make([]string, 0, 1024),
	}
}

// Put stores a Data packet in the cache.
// If the cache is full, evicts oldest entries until there is room.
// Returns false if the single item is larger than the entire cache.
func (cs *ContentStore) Put(d *Data) bool {
	key := d.Name.Key()
	size := estimateDataSize(d)

	if int64(size) > cs.maxBytes {
		log.Printf("[cs] item too large to cache: %s (%d bytes > max %d)", d.Name, size, cs.maxBytes)
		return false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	// If already in cache, remove old entry first (update case)
	if existing, ok := cs.entries[key]; ok {
		cs.curBytes -= int64(existing.sizeBytes)
		cs.removeFromOrder(key)
	}

	// Evict until we have room
	for cs.curBytes+int64(size) > cs.maxBytes && len(cs.insertOrder) > 0 {
		cs.evictOldest()
	}

	// Insert
	cs.entries[key] = &cacheEntry{
		data:       d,
		sizeBytes:  size,
		insertedAt: time.Now(),
		lastAccess: time.Now(),
	}
	cs.curBytes += int64(size)
	cs.insertOrder = append(cs.insertOrder, key)

	return true
}

// Get looks up a Data packet by name.
// Returns nil if not found or if MustBeFresh is true and data is stale.
// Updates the lastAccess time on hit.
func (cs *ContentStore) Get(name Name, mustBeFresh bool) *Data {
	key := name.Key()

	// Read lock — allows concurrent reads
	cs.mu.RLock()
	entry, ok := cs.entries[key]
	cs.mu.RUnlock()

	if !ok {
		return nil
	}

	// Check freshness without holding lock (IsFresh is read-only)
	if mustBeFresh && !entry.data.IsFresh() {
		// Stale — remove it (upgrade to write lock)
		cs.mu.Lock()
		// Re-check after acquiring write lock (another goroutine may have removed it)
		if e, stillThere := cs.entries[key]; stillThere && !e.data.IsFresh() {
			cs.evict(key)
		}
		cs.mu.Unlock()
		return nil
	}

	// Update access stats (upgrade to write lock for the update)
	cs.mu.Lock()
	if e, stillThere := cs.entries[key]; stillThere {
		e.lastAccess = time.Now()
		e.hits++
	}
	cs.mu.Unlock()

	return entry.data
}

// GetByPrefix returns all cached Data packets whose name starts with prefix.
// Used for CanBePrefix interests.
func (cs *ContentStore) GetByPrefix(prefix Name, mustBeFresh bool) []*Data {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var results []*Data
	for _, entry := range cs.entries {
		if entry.data.Name.HasPrefix(prefix) {
			if mustBeFresh && !entry.data.IsFresh() {
				continue
			}
			results = append(results, entry.data)
		}
	}
	return results
}

// Remove explicitly removes a named item from the cache.
// Used when we know data is outdated (e.g. file was replaced).
func (cs *ContentStore) Remove(name Name) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.evict(name.Key())
}

// Stats returns cache statistics for monitoring.
func (cs *ContentStore) Stats() ContentStoreStats {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	return ContentStoreStats{
		EntryCount:  len(cs.entries),
		UsedBytes:   cs.curBytes,
		MaxBytes:    cs.maxBytes,
		UsedPercent: float64(cs.curBytes) / float64(cs.maxBytes) * 100,
	}
}

// ContentStoreStats is returned by Stats() for health/monitoring endpoints.
type ContentStoreStats struct {
	EntryCount  int
	UsedBytes   int64
	MaxBytes    int64
	UsedPercent float64
}

// ── Private helpers ───────────────────────────────────────────────────────────
// These are called with cs.mu held (write lock).

func (cs *ContentStore) evictOldest() {
	if len(cs.insertOrder) == 0 {
		return
	}
	// Take the front of insertOrder (oldest)
	key := cs.insertOrder[0]
	cs.evict(key)
}

func (cs *ContentStore) evict(key string) {
	entry, ok := cs.entries[key]
	if !ok {
		return
	}
	cs.curBytes -= int64(entry.sizeBytes)
	delete(cs.entries, key)
	cs.removeFromOrder(key)
}

func (cs *ContentStore) removeFromOrder(key string) {
	/*
	  Linear scan to remove from the order slice.
	  O(n) but n is bounded by cache size and this is the write path.
	  For very large caches, a doubly-linked list would be O(1).
	  We'll optimize if profiling shows this is a bottleneck.
	*/
	for i, k := range cs.insertOrder {
		if k == key {
			cs.insertOrder = append(cs.insertOrder[:i], cs.insertOrder[i+1:]...)
			return
		}
	}
}

// estimateDataSize estimates the memory footprint of a Data packet in bytes.
// This is approximate — we care about rough size for cache pressure, not exactness.
func estimateDataSize(d *Data) int {
	size := len(d.Content)
	size += len(d.Name.Key())
	size += len(d.Signature)
	size += 256 // overhead for struct fields, timestamps, etc.
	return size
}