package arbitrage

import (
	"testing"
	"time"
)

func TestPriceCache(t *testing.T) {
	// Test basic cache operations
	pc := NewPriceCache()

	// Create test data
	tc1 := TriangleCache{
		SymbolA: PriceTuple{Ask: "100.0", Bid: "99.9"},
		SymbolB: PriceTuple{Ask: "200.0", Bid: "199.9"},
		SymbolC: PriceTuple{Ask: "300.0", Bid: "299.9"},
	}

	result1 := ArbitrageResult{
		found:  true,
		profit: 1.05,
		err:    nil,
	}

	// Test Set and Get
	pc.Set(tc1, result1)

	if retrieved, exists := pc.Get(tc1); !exists {
		t.Error("Expected to find cached result")
	} else if retrieved.profit != result1.profit {
		t.Errorf("Expected profit %f, got %f", result1.profit, retrieved.profit)
	}

	// Test cache size
	if pc.Size() != 1 {
		t.Errorf("Expected cache size 1, got %d", pc.Size())
	}

	// Test cache miss
	tc2 := TriangleCache{
		SymbolA: PriceTuple{Ask: "101.0", Bid: "100.9"},
		SymbolB: PriceTuple{Ask: "201.0", Bid: "200.9"},
		SymbolC: PriceTuple{Ask: "301.0", Bid: "300.9"},
	}

	if _, exists := pc.Get(tc2); exists {
		t.Error("Expected cache miss for different price combination")
	}
}

func TestPriceCacheConcurrency(t *testing.T) {
	pc := NewPriceCache()

	// Test concurrent access
	done := make(chan bool, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			tc := TriangleCache{
				SymbolA: PriceTuple{Ask: "100.0", Bid: "99.9"},
				SymbolB: PriceTuple{Ask: "200.0", Bid: "199.9"},
				SymbolC: PriceTuple{Ask: "300.0", Bid: "299.9"},
			}

			result := ArbitrageResult{
				found:  true,
				profit: float64(id),
				err:    nil,
			}

			pc.Set(tc, result)
			pc.Get(tc)
			pc.Size()

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Cache should have some entries
	if pc.Size() == 0 {
		t.Error("Expected cache to have entries after concurrent operations")
	}
}

func TestLRUCacheEviction(t *testing.T) {
	// Create a new cache
	cache := NewPriceCache()

	// Create test triangle caches
	tc1 := TriangleCache{
		SymbolA: PriceTuple{Ask: "1.0", Bid: "0.99"},
		SymbolB: PriceTuple{Ask: "2.0", Bid: "1.99"},
		SymbolC: PriceTuple{Ask: "3.0", Bid: "2.99"},
	}
	tc2 := TriangleCache{
		SymbolA: PriceTuple{Ask: "1.1", Bid: "1.09"},
		SymbolB: PriceTuple{Ask: "2.1", Bid: "2.09"},
		SymbolC: PriceTuple{Ask: "3.1", Bid: "3.09"},
	}
	tc3 := TriangleCache{
		SymbolA: PriceTuple{Ask: "1.2", Bid: "1.19"},
		SymbolB: PriceTuple{Ask: "2.2", Bid: "2.19"},
		SymbolC: PriceTuple{Ask: "3.2", Bid: "3.19"},
	}

	// Add entries to cache
	cache.Set(tc1, ArbitrageResult{found: true, profit: 1.01, err: nil})
	time.Sleep(10 * time.Millisecond) // Ensure different access times
	cache.Set(tc2, ArbitrageResult{found: true, profit: 1.02, err: nil})
	time.Sleep(10 * time.Millisecond)
	cache.Set(tc3, ArbitrageResult{found: true, profit: 1.03, err: nil})

	// Access tc1 to make it most recently used
	_, exists := cache.Get(tc1)
	if !exists {
		t.Error("tc1 should exist in cache")
	}

	// Verify all entries exist
	if cache.Size() != 3 {
		t.Errorf("Expected cache size 3, got %d", cache.Size())
	}

	// Manually trigger eviction of 2 entries (keeping only 1)
	cache.clearOldestEntries(2)

	// Verify only 1 entry remains
	if cache.Size() != 1 {
		t.Errorf("Expected cache size 1 after eviction, got %d", cache.Size())
	}

	// tc1 should still exist (most recently accessed)
	_, exists = cache.Get(tc1)
	if !exists {
		t.Error("tc1 should still exist after eviction (most recently used)")
	}

	// tc2 and tc3 should be evicted
	_, exists = cache.Get(tc2)
	if exists {
		t.Error("tc2 should be evicted")
	}

	_, exists = cache.Get(tc3)
	if exists {
		t.Error("tc3 should be evicted")
	}
}

func TestLRUCacheAccessTimeUpdate(t *testing.T) {
	cache := NewPriceCache()

	tc1 := TriangleCache{
		SymbolA: PriceTuple{Ask: "1.0", Bid: "0.99"},
		SymbolB: PriceTuple{Ask: "2.0", Bid: "1.99"},
		SymbolC: PriceTuple{Ask: "3.0", Bid: "2.99"},
	}

	// Add entry
	cache.Set(tc1, ArbitrageResult{found: true, profit: 1.01, err: nil})

	// Wait a bit
	time.Sleep(10 * time.Millisecond)

	// Access the entry
	_, exists := cache.Get(tc1)
	if !exists {
		t.Error("tc1 should exist in cache")
	}

	// The access time should be updated, making it the most recently used
	// This is tested implicitly by the eviction logic
}
