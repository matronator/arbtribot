package main

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/utils"
	"fmt"
	"os/exec"
	"sync"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/rs/zerolog/log"
)

var AccountBalances Balances
var cfg *utils.Config
var client *binance.Client
var OrderBook *arbitrage.Orderbook

func main() {
	cmd := exec.Command("scripts/populator")
	if err := cmd.Run(); err != nil {
		Error(err)
		WarningFmt("There was an error running the populator. Some pairs might be outdated.")
	}

	cfg = utils.LoadConfig()
	SetUpLogger()
	log.Info().
		Bool("SIMULATION_MODE", cfg.SimulationMode).
		Bool("DEBUG_MODE", cfg.DebugMode).
		Str("API_KEY", "***").
		Str("API_SECRET", "***").
		Float64("FEE_RATE", cfg.FeeRate).
		Float64("ORDER_USDC_AMOUNT", cfg.OrderUSDCAmount).
		Str("START_ASSET", cfg.StartAsset).
		Strs("BASE_ASSETS", cfg.BaseAssets).
		Msg("Bot started with config from .env file")
	if cfg.SimulationMode {
		InfoFmt("MODE: %s %s", Green("SIMULATION"), Italic(Dim("(no real trades will be placed, only logs)")))
	} else {
		InfoFmt("MODE: %s %s", Yellow("LIVE TRADING"), Italic(BgYellow("(real trades will be placed)")))
	}

	client = binance.NewClient(cfg.APIKey, cfg.APISecret)

	currency.FillPairs()

	OrderBook, err := arbitrage.FillOrderBook(client, cfg)
	if err != nil {
		Error(err)
		return
	}

	added, err := OrderBook.FillPrices(client)
	if err != nil {
		Error(err)
		ErrorFmt("OrderBook not filled. Added %d symbols", added)
		return
	}
	InfoFmt("OrderBook filled with %d symbols.", added)

	triangles := OrderBook.FindTriangles(cfg.FeeRate)
	InfoFmt("%s", Green(fmt.Sprintf("Found %d triangles!", len(triangles))))

	if !cfg.WdEnabled {
		ErrorFmt("%s", Red("[Binance error] Enable withdrawals for this token to continue with the current action."))
		WarningFmt("%s", Yellow("Starting on August 25th tokens will be required to have withdrawal permission in order to trade all USDC or USDT pairs."))
		return
	}

	if !cfg.SimulationMode {
		AccountBalances, err = CheckAccountBalance()
		if err != nil {
			Error(err)
		}
	}

	symbols := make([]string, 0, len(OrderBook.Symbols))
	for symbol := range OrderBook.Symbols {
		symbols = append(symbols, symbol)
	}

	// chunk := symbols[0:20]
	// ConnectToExchange(chunk, OrderBook)

	chunkSize := 50
	var wg sync.WaitGroup
	for i := 0; i < len(symbols); i += chunkSize {
		end := min(i+chunkSize, len(symbols))

		chunk := symbols[i:end]
		wg.Add(1)
		go func(symbolsChunk []string) {
			defer wg.Done()
			ConnectToExchange(symbolsChunk, OrderBook)
		}(chunk)
	}

	wg.Wait()

	// go loop(triangles, OrderBook)

	// quitChannel := make(chan os.Signal, 1)
	// signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
	// <-quitChannel

	defer func() {
		InfoFmt("Bot is shutting down...")
		InfoFmt("See you next time! %s", Blue("Arbtribot ended..."))
	}()
}

func loop(triangles []*arbitrage.Triangle, ob *arbitrage.Orderbook) {
	for {
		opportunities := make([]*arbitrage.Triangle, 0)
		for _, triangle := range triangles {
			found, profit, err := triangle.CheckArbitrage(cfg.FeeRate)
			if err != nil {
				Error(err)
				continue
			}
			if found || profit > 1.0105 {
				InfoFmt("%s %s - PROFIT: %g%%", Green("Arbitrage found!"), triangle, profit)
				opportunities = append(opportunities, triangle)
			}
		}

		if len(opportunities) <= 0 {
			InfoFmt("%s", Dim("No opportunities found this cycle."))
		} else {
			InfoFmt("%s", Green(fmt.Sprintf("%d opportunities found this cycle!", len(opportunities))))
		}

		executed := false
		if !cfg.SimulationMode {
			count := 0

			for _, t := range opportunities {
				if count < 5 {
					err := t.Execute(client, ob, cfg.OrderUSDCAmount)
					if err != nil {
						Error(err)
						continue
					}
					executed = true
					count++
				}
				break
			}
		}

		if executed {
			var err error
			AccountBalances, err = CheckAccountBalance()
			if err != nil {
				Error(err)
			}
		}

		time.Sleep(time.Second * 10)
		updated, err := ob.UpdatePrices(client)
		if err != nil {
			Error(err)
			continue
		}
		InfoFmt("Updated %d symbols.", updated)
	}
}
