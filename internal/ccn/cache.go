package ccn

import (
	"log"
	"sync"
	"time"
)

type cacheEntry struct {
	data       *Data
	sizeBytes  int
	insertedAt time.Time
	lastAccess time.Time
	hits       int
}

type ContentStore struct {
	mu       sync.RWMutex
	entries  map[string]*cacheEntry
	maxBytes int64
	curBytes int64

	insertOrder []string
}

func NewContentStore(maxBytes int64) *ContentStore {
	if maxBytes <= 0 {
		maxBytes = 256 * 1024 * 1024
	}
	return &ContentStore{
		entries:     make(map[string]*cacheEntry),
		maxBytes:    maxBytes,
		insertOrder: make([]string, 0, 1024),
	}
}

func (cs *ContentStore) Put(d *Data) bool {
	key := d.Name.Key()
	size := estimateDataSize(d)

	if int64(size) > cs.maxBytes {
		log.Printf("[cs] item too large to cache: %s (%d bytes > max %d)", d.Name, size, cs.maxBytes)
		return false
	}

	cs.mu.Lock()
	defer cs.mu.Unlock()

	if existing, ok := cs.entries[key]; ok {
		cs.curBytes -= int64(existing.sizeBytes)
		cs.removeFromOrder(key)
	}

	for cs.curBytes+int64(size) > cs.maxBytes && len(cs.insertOrder) > 0 {
		cs.evictOldest()
	}

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

func (cs *ContentStore) Get(name Name, mustBeFresh bool) *Data {
	key := name.Key()

	cs.mu.RLock()
	entry, ok := cs.entries[key]
	cs.mu.RUnlock()

	if !ok {
		return nil
	}

	if mustBeFresh && !entry.data.IsFresh() {

		cs.mu.Lock()

		if e, stillThere := cs.entries[key]; stillThere && !e.data.IsFresh() {
			cs.evict(key)
		}
		cs.mu.Unlock()
		return nil
	}

	cs.mu.Lock()
	if e, stillThere := cs.entries[key]; stillThere {
		e.lastAccess = time.Now()
		e.hits++
	}
	cs.mu.Unlock()

	return entry.data
}

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

func (cs *ContentStore) Remove(name Name) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.evict(name.Key())
}

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

type ContentStoreStats struct {
	EntryCount  int
	UsedBytes   int64
	MaxBytes    int64
	UsedPercent float64
}

func (cs *ContentStore) evictOldest() {
	if len(cs.insertOrder) == 0 {
		return
	}

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

	for i, k := range cs.insertOrder {
		if k == key {
			cs.insertOrder = append(cs.insertOrder[:i], cs.insertOrder[i+1:]...)
			return
		}
	}
}

func estimateDataSize(d *Data) int {
	size := len(d.Content)
	size += len(d.Name.Key())
	size += len(d.Signature)
	size += 256
	return size
}
