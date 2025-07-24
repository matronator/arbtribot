package main

import (
	"fmt"
	"sort"
)

// ArbitrageOpportunity holds a detected arbitrage path and profit info
type ArbitrageOpportunity struct {
	Path        [3]string
	Profit      float64
	FinalAmount float64
}

// FindTriangularArbitrage brute-forces all possible 3-pair cycles and detects arbitrage
func FindTriangularArbitrage(
	obm *OrderBookManager,
	pairs []string,
	baseCurrency string,
	fee float64,
	minProfit float64,
) []ArbitrageOpportunity {
	var results []ArbitrageOpportunity

	for _, pair1 := range pairs {
		for _, pair2 := range pairs {
			for _, pair3 := range pairs {
				fmt.Printf("Trying Path: %s -> %s -> %s (before checking order books)\n", pair1, pair2, pair3)

				// Skip duplicates
				if pair1 == pair2 || pair2 == pair3 || pair1 == pair3 {
					continue
				}

				// Get order books for all 3 pairs
				ob1, ok1 := obm.Get(pair1)
				ob2, ok2 := obm.Get(pair2)
				ob3, ok3 := obm.Get(pair3)
				if !(ok1 && ok2 && ok3) {
					continue
				}

				// Debug log: show what we’re testing
				fmt.Printf("Testing Path: %s -> %s -> %s | Prices: Ask1=%.8f Bid2=%.8f Bid3=%.8f\n",
					pair1, pair2, pair3, ob1.BestAsk, ob2.BestBid, ob3.BestBid)

				// Simulate trading cycle with 1 unit of base currency
				startAmount := 1.0
				amt1 := (startAmount / ob1.BestAsk) * (1 - fee)
				amt2 := (amt1 * ob2.BestBid) * (1 - fee)
				finalAmount := (amt2 * ob3.BestBid) * (1 - fee)

				profit := finalAmount - startAmount

				if profit > minProfit {
					results = append(results, ArbitrageOpportunity{
						Path:        [3]string{pair1, pair2, pair3},
						Profit:      profit,
						FinalAmount: finalAmount,
					})
				}
			}
		}
	}

	// Sort by highest profit
	sort.Slice(results, func(i, j int) bool {
		return results[i].Profit > results[j].Profit
	})

	return results
}
