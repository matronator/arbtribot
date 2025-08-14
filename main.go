package main

import (
	"arbitrage/arbitrage"
	"arbitrage/currency"
	"arbitrage/utils"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

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

	OrderBook, err := arbitrage.FillOrderBook(client)
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
	// for _, triangle := range triangles {
	// 	found, profit, err := triangle.CheckArbitrage(cfg.FeeRate)
	// 	if err != nil {
	// 		Error(err)
	// 		continue
	// 	}
	// 	InfoFmt("Found: %t - %s - PROFIT: %g%%", found, triangle, profit)
	// }
	InfoFmt("%s", Green(fmt.Sprintf("Found %d triangles!", len(triangles))))

	if !cfg.SimulationMode {
		AccountBalances, err = CheckAccountBalance()
		if err != nil {
			Error(err)
		}

		go loop(triangles, OrderBook)

		quitChannel := make(chan os.Signal, 1)
		signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)
		<-quitChannel

		// ConvertAllToUSDC()

		// ConnectToExchange()
	}

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
			if found || profit > 0.998 {
				InfoFmt("%s %s - PROFIT: %g%%", Green("Arbitrage found!"), triangle, profit)
				opportunities = append(opportunities, triangle)
			}
		}

		if len(opportunities) <= 0 {
			InfoFmt("%s", Dim("No opportunities found this cycle."))
		} else {
			InfoFmt("%s", Green(fmt.Sprintf("%d opportunities found this cycle!", len(opportunities))))
		}

		count := 0

		for _, t := range opportunities {
			if count < 5 {
				err := t.Execute(client, ob, 10)
				if err != nil {
					Error(err)
					continue
				}
				count++
			}
			break
		}

		time.Sleep(time.Second * 5)
		updated, err := ob.UpdatePrices(client)
		if err != nil {
			Error(err)
			continue
		}
		InfoFmt("Updated %d symbols.", updated)
	}
}
