package arbitrage

import (
	"sync"
	"time"
)

type PriceTuple struct {
	Ask string
	Bid string
}

type TriangleCache struct {
	SymbolA PriceTuple
	SymbolB PriceTuple
	SymbolC PriceTuple
}

type ArbitrageResult struct {
	found  bool
	profit float64
	err    error
}

type CacheEntry struct {
	result     ArbitrageResult
	lastAccess time.Time
}

type PriceCache struct {
	cache map[TriangleCache]CacheEntry
	mutex sync.RWMutex
}

// NewPriceCache creates a new price cache with size limit
func NewPriceCache() *PriceCache {
	return &PriceCache{
		cache: make(map[TriangleCache]CacheEntry),
		mutex: sync.RWMutex{},
	}
}

// Get retrieves a cached result for the given price combination
func (pc *PriceCache) Get(tc TriangleCache) (ArbitrageResult, bool) {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	entry, exists := pc.cache[tc]
	if !exists {
		return ArbitrageResult{}, false
	}

	// Update access time for LRU tracking
	entry.lastAccess = time.Now()
	pc.cache[tc] = entry

	return entry.result, true
}

// Set stores a result for the given price combination
func (pc *PriceCache) Set(tc TriangleCache, result ArbitrageResult) {
	pc.mutex.Lock()
	defer pc.mutex.Unlock()

	// Simple size limit to prevent memory leaks
	const maxCacheSize = 1000
	if len(pc.cache) >= maxCacheSize {
		// Clear half the cache using LRU strategy
		pc.clearOldestEntries(len(pc.cache) / 2)
	}

	pc.cache[tc] = CacheEntry{
		result:     result,
		lastAccess: time.Now(),
	}
}

// clearOldestEntries removes the oldest entries from the cache using LRU strategy
func (pc *PriceCache) clearOldestEntries(count int) {
	if count <= 0 || len(pc.cache) == 0 {
		return
	}

	// Create a slice to store cache entries with their keys for sorting
	type cacheItem struct {
		key        TriangleCache
		lastAccess time.Time
	}

	var items []cacheItem
	for key, entry := range pc.cache {
		items = append(items, cacheItem{
			key:        key,
			lastAccess: entry.lastAccess,
		})
	}

	// Sort by last access time (oldest first)
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[i].lastAccess.After(items[j].lastAccess) {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	// Remove the oldest entries
	removed := 0
	for _, item := range items {
		if removed >= count {
			break
		}
		delete(pc.cache, item.key)
		removed++
	}
}

// Size returns the current cache size
func (pc *PriceCache) Size() int {
	pc.mutex.RLock()
	defer pc.mutex.RUnlock()
	return len(pc.cache)
}
