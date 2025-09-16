package main

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var AccountBalances Balances
var cfg *utils.Config
var client *binance.Client
var OrderBook *arbitrage.Orderbook
var SimTradeLogger zerolog.Logger

func main() {
	cmd := exec.Command("scripts/populator")
	if err := cmd.Run(); err != nil {
		logger.Error(err)
		logger.WarningFmt("There was an error running the populator. Some pairs might be outdated.")
	}

	cfg = utils.LoadConfig()
	SetUpLogger()
	SimTradeLogger = logger.NewSimTradeWriter()
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
		logger.InfoFmt("MODE: %s %s", logger.Green("SIMULATION"), logger.Italic(logger.Dim("(no real trades will be placed, only logs)")))
	} else {
		logger.InfoFmt("MODE: %s %s", logger.Yellow("LIVE TRADING"), logger.Italic(logger.BgYellow("(real trades will be placed)")))
	}

	client = binance.NewClient(cfg.APIKey, cfg.APISecret)

	currency.FillPairs()

	OrderBook, err := arbitrage.FillOrderBook(client, cfg, &SimTradeLogger)
	if err != nil {
		logger.Error(err)
		return
	}

	added, err := OrderBook.FillPrices()
	if err != nil {
		logger.Error(err)
		logger.ErrorFmt("OrderBook not filled. Added %d symbols", added)
		return
	}
	logger.InfoFmt("OrderBook filled with %d symbols.", added)

	triangles := OrderBook.FindTriangles()
	logger.InfoFmt("%s", logger.Green(fmt.Sprintf("Found %d triangles!", len(triangles))))

	if !cfg.WdEnabled {
		logger.ErrorFmt("%s", logger.Red("[Binance error] Enable withdrawals for this token to continue with the current action."))
		logger.WarningFmt("%s", logger.Yellow("Starting on August 25th tokens will be required to have withdrawal permission in order to trade all USDC or USDT pairs."))
		return
	}

	AccountBalances, err = CheckAccountBalance()
	if err != nil {
		logger.Error(err)
		return
	}

	symbols := make([]string, 0, OrderBook.Symbols.Count())
	for symbol := range OrderBook.Symbols.IterBuffered() {
		symbols = append(symbols, symbol.Key)
	}

	websocketStreamClient := binance.NewWebsocketStreamClient(true)
	handler := NewBookTickerHandler(OrderBook)

	chunkSize := 150
	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	errCh := make(chan error)

	for i := 0; i < len(symbols); i += chunkSize {
		end := min(i+chunkSize, len(symbols))
		chunk := symbols[i:end]

		wg.Add(1)
		go func(symbolsChunk []string) {
			defer wg.Done()
			logger.InfoFmt("Starting websocket stream for %d symbols.", len(symbolsChunk))
			doneCh, stop, err := websocketStreamClient.WsCombinedBookTickerServe(
				symbolsChunk,
				handler.HandleBookTickerEvent,
				handler.HandleError,
			)
			if err != nil {
				errCh <- err
				return
			}

			// Wait for stop signal
			select {
			case <-stopCh:
				stop <- struct{}{}
			case <-doneCh:
				return
			}
		}(chunk)
	}

	time.Sleep(time.Millisecond * 15)
	logger.InfoFmt("%s", logger.Reset()+logger.BrightYellow(logger.Italic("Looking for arbitrage opportunities...")))

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either an error or an interrupt signal
	select {
	case err := <-errCh:
		logger.Error(err)
		close(stopCh) // Signal all goroutines to stop
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing connections...")
		close(stopCh) // Signal all goroutines to stop
	}

	wg.Wait()

	defer func() {
		logger.InfoFmt("Bot is shutting down...")
		logger.InfoFmt("See you next time! %s", logger.Blue("Arbtribot ended..."))
	}()
}

func loop(triangles []*arbitrage.Triangle, ob *arbitrage.Orderbook) {
	for {
		opportunities := make([]*arbitrage.Triangle, 0)
		for _, triangle := range triangles {
			found, profit, err := triangle.CheckArbitrage(ob, cfg.FeeRate)
			if err != nil {
				logger.Error(err)
				continue
			}
			if found || profit > 1.0105 {
				logger.InfoFmt("%s %s - PROFIT: %g%%", logger.Green("Arbitrage found!"), triangle, profit)
				opportunities = append(opportunities, triangle)
			}
		}

		if len(opportunities) <= 0 {
			logger.InfoFmt("%s", logger.Dim("No opportunities found this cycle."))
		} else {
			logger.InfoFmt("%s", logger.Green(fmt.Sprintf("%d opportunities found this cycle!", len(opportunities))))
		}

		executed := false
		if !cfg.SimulationMode {
			count := 0

			for _, t := range opportunities {
				if count < 5 {
					locked, err := t.Execute(ob, cfg.OrderUSDCAmount)
					if err != nil {
						logger.Error(err)
						continue
					}
					if locked {
						logger.WarningFmt("Triangle %s LOCKED from trading.", t.String())
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
				logger.Error(err)
			}
		}

		time.Sleep(time.Second * 10)
		updated, err := ob.UpdatePrices()
		if err != nil {
			logger.Error(err)
			continue
		}
		logger.InfoFmt("Updated %d symbols.", updated)
	}
}
