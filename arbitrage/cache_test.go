package arbitrage

import (
	"testing"
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
