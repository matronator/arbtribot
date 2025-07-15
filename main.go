package main

import (
	"fmt"
	"time"
)

func main() {
	// Load config (toggle simulation/live here)
	cfg := LoadConfig()
	fmt.Println("Starting triangular arbitrage bot")
	if cfg.SimulationMode {
		fmt.Println("MODE: SIMULATION (no real trades will be placed)")
	} else {
		fmt.Println("MODE: LIVE TRADING (real trades WILL be placed!)")
	}

	// Fetch top 200 pairs by volume
	fmt.Println("Fetching top 200 trading pairs by volume...")
	pairs, err := FetchTopPairs(200)
	if err != nil {
		fmt.Println("Error fetching pairs:", err)
		return
	}
	fmt.Printf("Top 200 pairs fetched: %d\n", len(pairs))

	// Initialize order book manager
	obm := NewOrderBookManager()

	// Start WebSocket streams for pairs (non-blocking)
	err = StartWebSocketStreams(pairs, obm)
	if err != nil {
		fmt.Println("Error starting WebSocket streams:", err)
		return
	}
	fmt.Println("WebSocket streams started. Receiving live order book updates...")

	// Setup trade executor based on mode
	var executor TradeExecutor
	if cfg.SimulationMode {
		executor = &SimulatedExecutor{}
	} else {
		executor = NewBinanceExecutor(cfg.APIKey, cfg.APISecret)
	}

	// Run arbitrage detection loop
	baseCurrency := "USDT" // you can make configurable later
	fee := cfg.FeeRate
	minProfit := -999.9 // minimum profit threshold to act on (adjust as needed)

	fmt.Println("Starting arbitrage detection loop...")
	ticker := time.NewTicker(5 * time.Second) // check every 5 seconds

	for range ticker.C {
		// Find arbitrage opportunities
		opps := FindTriangularArbitrage(obm, pairs, baseCurrency, fee, minProfit)

		if len(opps) == 0 {
			fmt.Println("No arbitrage opportunities detected this cycle.")
			continue
		}

		best := opps[0]
		fmt.Printf("Arbitrage opportunity found: Path=%v Profit=%.6f FinalAmount=%.6f\n",
			best.Path, best.Profit, best.FinalAmount)

		// Execute or simulate trades
		err := executor.ExecuteTriangularArbitrage(best.Path, 1.0) // trading 1 unit base currency (configurable)
		if err != nil {
			fmt.Println("Trade execution error:", err)
		} else {
			fmt.Println("Trade executed successfully.")
		}
	}
}
