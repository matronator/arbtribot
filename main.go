package main

import (
	"arbitrage/currency"
	"arbitrage/orderbook"
	"arbitrage/utils"
	"os/exec"

	binance "github.com/binance/binance-connector-go"
	"github.com/rs/zerolog/log"
)

var AccountBalances Balances
var cfg *utils.Config
var client *binance.Client

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
		Msg("Bot started with config from .env file")
	if cfg.SimulationMode {
		InfoFmt("MODE: %s %s", Green("SIMULATION"), Italic(Dim("(no real trades will be placed, only logs)")))
	} else {
		InfoFmt("MODE: %s %s", Yellow("LIVE TRADING"), Italic(BgYellow("(real trades will be placed)")))
	}

	client = binance.NewClient(cfg.APIKey, cfg.APISecret)

	currency.FillPairs()

	OrderBook, err := orderbook.FillOrderBook(client)
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
	for _, triangle := range triangles {
		found, profit, err := triangle.CheckArbitrage(cfg.FeeRate)
		if err != nil {
			Error(err)
			continue
		}
		InfoFmt("Found: %t - %s - PROFIT: %g%%", found, triangle, profit)
	}

	if !cfg.SimulationMode {
		AccountBalances, err = CheckAccountBalance()
		if err != nil {
			Error(err)
		}

		// ConvertAllToUSDC()

		// ConnectToExchange()
	}

	defer func() {
		InfoFmt("Bot is shutting down...")
		InfoFmt("See you next time! %s", Blue("Arbtribot ended..."))
	}()
}
