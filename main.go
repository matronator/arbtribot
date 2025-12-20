package main

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/grid"
	"arbtribot/logger"
	"arbtribot/trading"
	"arbtribot/utils"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	futures "github.com/adshao/go-binance/v2/futures"
	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

var AccountBalances Balances
var cfg *utils.Config
var client *binance.Client
var futuresClient *futures.Client
var OrderBook *arbitrage.Orderbook
var SimTradeLogger zerolog.Logger
var GridBot *grid.GridTradingBot
var TradingBot *trading.TradingBot
var FuturesBot *trading.FuturesBot
var MarginBot *trading.MarginBot
var positionsLoaded = false

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
		Bool("SIMULATION_MODE", cfg.GeneralConfig.SimulationMode).
		Bool("DEBUG_MODE", cfg.GeneralConfig.DebugMode).
		Bool("FUTURES", cfg.GeneralConfig.Futures).
		Str("API_KEY", "***").
		Str("API_SECRET", "***").
		Float64("FEE_RATE", cfg.GeneralConfig.FeeRate).
		Float64("ORDER_USDC_AMOUNT", cfg.TriangleConfig.OrderUSDCAmount).
		Float64("SIMULATION_USDC_AMOUNT", cfg.TriangleConfig.SimulationUSDCAmount).
		Str("START_ASSET", cfg.TriangleConfig.StartAsset).
		Strs("BASE_ASSETS", cfg.TriangleConfig.BaseAssets).
		Str("TRADING_MODE", cfg.GeneralConfig.TradingMode).
		Msg("Bot started with config from .env file")
	if cfg.GeneralConfig.SimulationMode {
		logger.InfoFmt("MODE: %s %s", logger.Green("SIMULATION"), logger.Italic(logger.Dim("(no real trades will be placed, only logs)")))
	} else {
		logger.InfoFmt("MODE: %s %s", logger.Yellow("LIVE TRADING"), logger.Italic(logger.BgYellow("(real trades will be placed)")))
	}

	client = binance.NewClient(cfg.GeneralConfig.APIKey, cfg.GeneralConfig.APISecret)
	if cfg.GeneralConfig.Futures {
		cfg.GeneralConfig.TradingMode = "futures"
		futuresClient = futures.NewClient(cfg.GeneralConfig.APIKey, cfg.GeneralConfig.APISecret)
		logger.InfoFmt("%s", logger.Green("FUTURES=true detected. Using futures trading mode and endpoints."))
	}

	currency.FillPairs()

	OrderBook, err := arbitrage.FillOrderBook(client, futuresClient, cfg, &SimTradeLogger)
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

	// Check trading mode and start appropriate strategy
	switch cfg.GeneralConfig.TradingMode {
	case "grid":
		logger.InfoFmt("%s", logger.Green("Starting Grid Trading Mode..."))
		startGridTrading(OrderBook)
	case "triangle":
		logger.InfoFmt("%s", logger.Green("Starting Triangle Arbitrage Mode..."))
		startTriangleArbitrage()
	case "futures":
		logger.InfoFmt("%s", logger.Green("Starting Futures Trading Mode..."))
		startFuturesTrading(OrderBook)
	case "margin":
		logger.InfoFmt("%s", logger.Green("Starting Margin Trading Mode..."))
		startMarginTrading(OrderBook)
	case "normal":
		logger.InfoFmt("%s", logger.Green(fmt.Sprintf("Starting %s...", "Normal Trading Mode")))
		startNormalTrading(OrderBook)
	}

	defer func() {
		logger.InfoFmt("Bot is shutting down...")
		logger.InfoFmt("See you next time! %s", logger.Blue("Arbtribot ended..."))
	}()
}

func startNormalTrading(ob *arbitrage.Orderbook) {
	// ConvertToUSDC("BANANAS31", "1850")
	// ConvertToUSDC("DOGE", "41")
	// ConvertToUSDC("SOL", "0.131") // = 70 USDC

	// ConvertUSDCToBTC("70") // = 0.0006305853250633569 BTC

	// err := WithdrawBTC("0.0006", "bc1qlpft888fndt48fvz2c4dryndzu4657xdvedemq")
	// if err != nil {
	// 	logger.Error(err)
	// 	return
	// }

	// _, err = CheckAccountBalance()
	// if err != nil {
	// 	logger.Error(err)
	// 	return
	// }

	// return

	TradingBot = trading.NewTradingBot(ob, &SimTradeLogger)

	// Load saved positions after bot initialization
	gridPositionsMap := make(map[string]interface{})
	if err := trading.LoadPositions(TradingBot, MarginBot, FuturesBot, gridPositionsMap); err != nil {
		logger.WarningFmt("Failed to load saved positions: %v", err)
	}

	symbols := make([]string, 0, len(cfg.NormalConfig.QuoteAssets))
	for _, asset := range cfg.NormalConfig.QuoteAssets {
		symbols = append(symbols, asset+cfg.NormalConfig.BaseAsset)
	}

	websocketStreamClient := binance.NewWebsocketStreamClient(false)
	handler := NewBookTickerHandler(ob)

	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	errCh := make(chan error)

	for i := 0; i < len(symbols); i += 1 {
		symbol := symbols[i]
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			logger.InfoFmt("Starting websocket stream for %s.", symbol)
			doneCh, stop, err := websocketStreamClient.WsBookTickerServe(
				symbol,
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
		}(symbol)
	}

	time.Sleep(time.Millisecond * 15)
	logger.InfoFmt("%s", logger.Reset()+logger.BrightYellow(logger.Italic("Normal trading bot is running...")))

	TradingBot.StartTrading()

	// Start periodic position saving
	positionSaveTicker := time.NewTicker(30 * time.Second)
	positionSaveStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-positionSaveTicker.C:
				if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
					logger.WarningFmt("Failed to save positions periodically: %v", err)
				}
			case <-positionSaveStop:
				return
			}
		}
	}()

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		logger.Error(err)
		close(stopCh) // Signal all goroutines to stop
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing connections...")
		close(stopCh) // Signal all goroutines to stop
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
	}

	wg.Wait()
}

// startFuturesTrading initializes and starts the directional futures bot.
func startFuturesTrading(ob *arbitrage.Orderbook) {
	if futuresClient == nil {
		logger.ErrorFmt("Futures client not initialized. Ensure FUTURES=true and credentials are set.")
		return
	}

	FuturesBot = trading.NewFuturesBot(ob, cfg, futuresClient, &SimTradeLogger)

	// Load saved positions after bot initialization
	gridPositionsMap := make(map[string]interface{})
	if err := trading.LoadPositions(TradingBot, MarginBot, FuturesBot, gridPositionsMap); err != nil {
		logger.WarningFmt("Failed to load saved positions: %v", err)
	}

	symbols := make([]string, 0, len(cfg.FuturesConfig.QuoteAssets))
	for _, asset := range cfg.FuturesConfig.QuoteAssets {
		if asset == "" {
			continue
		}
		symbols = append(symbols, asset+cfg.FuturesConfig.BaseAsset)
	}

	if len(symbols) == 0 {
		logger.WarningFmt("No futures symbols configured. Check FUTURES_QUOTE_ASSETS and FUTURES_BASE_ASSET.")
		return
	}

	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	errCh := make(chan error)

	for i := 0; i < len(symbols); i++ {
		symbol := symbols[i]
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			logger.InfoFmt("Starting futures websocket stream for %s.", symbol)
			doneCh, stop, err := futures.WsBookTickerServe(
				symbol,
				func(event *futures.WsBookTickerEvent) {
					book := &arbitrage.BookTicker{
						AskPrice: event.BestAskPrice,
						AskQty:   event.BestAskQty,
						BidPrice: event.BestBidPrice,
						BidQty:   event.BestBidQty,
						UpdateID: event.TransactionTime,
					}
					_, _ = ob.UpdateBookTicker(event.Symbol, book)
				},
				func(err error) {
					errCh <- err
				},
			)
			if err != nil {
				errCh <- err
				return
			}

			select {
			case <-stopCh:
				stop <- struct{}{}
			case <-doneCh:
				return
			}
		}(symbol)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	time.Sleep(time.Millisecond * 15)
	logger.InfoFmt("%s", logger.Reset()+logger.BrightYellow(logger.Italic("Futures trading bot is running...")))
	FuturesBot.Start(ctx)

	// Start periodic position saving
	positionSaveTicker := time.NewTicker(30 * time.Second)
	positionSaveStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-positionSaveTicker.C:
				if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
					logger.WarningFmt("Failed to save positions periodically: %v", err)
				}
			case <-positionSaveStop:
				return
			}
		}
	}()

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		logger.Error(err)
		close(stopCh)
		cancel()
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("futures")
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing futures streams...")
		close(stopCh)
		cancel()
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("futures")
	}

	wg.Wait()

	// Log final simulation summary if in simulation mode
	if cfg.GeneralConfig.SimulationMode && FuturesBot != nil {
		FuturesBot.LogFinalSimulationSummary()
	}
}

// startMarginTrading initializes and starts the directional margin bot.

func startMarginTrading(ob *arbitrage.Orderbook) {
	if client == nil {
		logger.ErrorFmt("Spot client not initialized. Ensure credentials are set.")
		return
	}

	MarginBot = trading.NewMarginBot(ob, cfg, client, &SimTradeLogger)

	// Load saved positions after bot initialization
	gridPositionsMap := make(map[string]interface{})
	if err := trading.LoadPositions(TradingBot, MarginBot, FuturesBot, gridPositionsMap); err != nil {
		logger.WarningFmt("Failed to load saved positions: %v", err)
		positionsLoaded = false
	} else {
		positionsLoaded = true
	}

	symbols := make([]string, 0, len(cfg.MarginConfig.QuoteAssets))
	for _, asset := range cfg.MarginConfig.QuoteAssets {
		if asset == "" {
			continue
		}
		symbols = append(symbols, asset+cfg.MarginConfig.BaseAsset)
	}

	if len(symbols) == 0 {
		logger.WarningFmt("No margin symbols configured. Check MARGIN_QUOTE_ASSETS and MARGIN_BASE_ASSET.")
		return
	}

	websocketStreamClient := binance.NewWebsocketStreamClient(false)
	handler := NewBookTickerHandler(ob)

	var wg sync.WaitGroup
	stopCh := make(chan struct{})
	errCh := make(chan error)
	symbolsStr := strings.Join(symbols, ", ")
	logger.InfoFmt("Starting margin websocket streams for %s.", symbolsStr)

	for i := 0; i < len(symbols); i++ {
		symbol := symbols[i]
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			doneCh, stop, err := websocketStreamClient.WsBookTickerServe(
				symbol,
				handler.HandleBookTickerEvent,
				handler.HandleError,
			)
			if err != nil {
				errCh <- err
				return
			}

			select {
			case <-stopCh:
				stop <- struct{}{}
			case <-doneCh:
				return
			}
		}(symbol)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	time.Sleep(time.Millisecond * 15)
	logger.InfoFmt("%s", logger.Reset()+logger.BrightYellow(logger.Italic("Margin trading bot is running...")))
	MarginBot.Start(ctx)

	// Start periodic position saving
	positionSaveTicker := time.NewTicker(30 * time.Second)
	positionSaveStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-positionSaveTicker.C:
				if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
					logger.WarningFmt("Failed to save positions periodically: %v", err)
				}
			case <-positionSaveStop:
				return
			}
		}
	}()

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	select {
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing margin streams...")
		close(stopCh)
		cancel()
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("margin")
	case err := <-errCh:
		logger.Error(err)
		close(stopCh)
		cancel()
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("margin")
	}

	wg.Wait()

	// Log final simulation summary if in simulation mode
	if MarginBot != nil {
		MarginBot.LogFinalSimulationSummary()
	}
}

// saveRunStatistics collects and saves statistics for the current run
func saveRunStatistics(tradingMode string) {
	stats := collectRunStatistics(tradingMode)
	if err := stats.SaveToCSV(); err != nil {
		logger.ErrorFmt("Failed to save run statistics: %v", err)
	} else {
		logger.InfoFmt("Run statistics saved to: %s", fmt.Sprintf("stats_%s_%s.csv", tradingMode, stats.RunID))
	}
}

// collectRunStatistics gathers statistics from the active trading bot
func collectRunStatistics(tradingMode string) *utils.RunStatistics {
	stats := &utils.RunStatistics{
		RunID:       generateRunID(),
		StartTime:   time.Now(),
		EndTime:     time.Now(),
		Config:      cfg,
		TradingMode: tradingMode,
		Orders:      make([]utils.OrderRecord, 0),
	}

	// Collect statistics based on trading mode
	switch tradingMode {
	case "margin":
		if MarginBot != nil {
			collectMarginStats(stats, MarginBot)
		}
	case "futures":
		if FuturesBot != nil {
			collectFuturesStats(stats, FuturesBot)
		}
	case "normal":
		if TradingBot != nil {
			collectNormalStats(stats, TradingBot)
		}
	}

	return stats
}

func collectMarginStats(stats *utils.RunStatistics, bot *trading.MarginBot) {
	simStats := bot.GetSimStats()
	if simStats != nil {
		stats.StartTime = simStats.StartTime
		stats.TotalTrades = simStats.TotalTrades
		stats.WinningTrades = simStats.WinningTrades
		stats.LosingTrades = simStats.LosingTrades
		stats.TotalPnL = simStats.TotalPnL.StringFixed(4)
		stats.TotalPnLPercent = simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)
	}

	// Collect individual orders from closed positions
	closedPositions := bot.ClosedPositions()
	for symbol, positions := range closedPositions {
		for _, cp := range positions {
			stats.Orders = append(stats.Orders, utils.OrderRecord{
				Symbol:       symbol,
				Side:         cp.EntryPosition.Side,
				EntryPrice:   cp.EntryPosition.EntryPrice.StringFixed(8),
				ExitPrice:    cp.ClosingPrice,
				Quantity:     cp.EntryPosition.Quantity.StringFixed(8),
				EntryTime:    cp.EntryPosition.EntryTime,
				ExitTime:     cp.ClosingTime,
				PnL:          cp.Profit,
				PnLPercent:   cp.PnLPercent,
				Reason:       cp.Reason,
				HoldDuration: cp.HoldDuration,
				Notional:     cp.EntryPosition.Notional.StringFixed(2),
				MarginType:   cfg.MarginConfig.MarginType,
			})
		}
	}
}

func collectFuturesStats(stats *utils.RunStatistics, bot *trading.FuturesBot) {
	simStats := bot.GetSimStats()
	if simStats != nil {
		stats.StartTime = simStats.StartTime
		stats.TotalTrades = simStats.TotalTrades
		stats.WinningTrades = simStats.WinningTrades
		stats.LosingTrades = simStats.LosingTrades
		stats.TotalPnL = simStats.TotalPnL.StringFixed(4)
		stats.TotalPnLPercent = simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)
	}

	// Collect individual orders from closed positions
	closedPositions := bot.ClosedPositions()
	for symbol, positions := range closedPositions {
		for _, cp := range positions {
			stats.Orders = append(stats.Orders, utils.OrderRecord{
				Symbol:       symbol,
				Side:         cp.EntryPosition.Side,
				EntryPrice:   cp.EntryPosition.EntryPrice.StringFixed(8),
				ExitPrice:    cp.ClosingPrice,
				Quantity:     cp.EntryPosition.Quantity.StringFixed(8),
				EntryTime:    cp.EntryPosition.EntryTime,
				ExitTime:     cp.ClosingTime,
				PnL:          cp.Profit,
				PnLPercent:   cp.PnLPercent,
				Reason:       cp.Reason,
				HoldDuration: cp.HoldDuration,
				Notional:     cp.EntryPosition.Notional.StringFixed(2),
				MarginType:   "", // Futures doesn't have margin type
			})
		}
	}
}

func collectNormalStats(stats *utils.RunStatistics, bot *trading.TradingBot) {
	bot.Mu().RLock()
	defer bot.Mu().RUnlock()

	totalPnL := 0.0
	totalVolume := 0.0
	totalTrades := 0
	winningTrades := 0
	losingTrades := 0

	for symbol, closedPositions := range bot.ClosedPositions {
		for _, cp := range closedPositions {
			profit, err := strconv.ParseFloat(cp.Profit, 64)
			if err != nil {
				continue
			}

			entryPrice, err := strconv.ParseFloat(cp.EntryPosition.EntryPrice, 64)
			if err != nil {
				continue
			}

			quantity, err := strconv.ParseFloat(cp.EntryPosition.Quantity, 64)
			if err != nil {
				continue
			}

			volume := entryPrice * quantity
			totalVolume += volume
			totalPnL += profit
			totalTrades++

			if profit > 0 {
				winningTrades++
			} else if profit < 0 {
				losingTrades++
			}

			pnlPercent := 0.0
			if volume > 0 {
				pnlPercent = (profit / volume) * 100
			}

			holdDuration := cp.ClosingTime.Sub(cp.EntryPosition.EntryTime)

			stats.Orders = append(stats.Orders, utils.OrderRecord{
				Symbol:       symbol,
				Side:         "LONG",
				EntryPrice:   cp.EntryPosition.EntryPrice,
				ExitPrice:    cp.ClosingPrice,
				Quantity:     cp.EntryPosition.Quantity,
				EntryTime:    cp.EntryPosition.EntryTime,
				ExitTime:     cp.ClosingTime,
				PnL:          cp.Profit,
				PnLPercent:   fmt.Sprintf("%.4f", pnlPercent),
				Reason:       "CLOSED",
				HoldDuration: holdDuration.Round(time.Second).String(),
				Notional:     fmt.Sprintf("%.2f", volume),
			})
		}
	}

	stats.TotalTrades = totalTrades
	stats.WinningTrades = winningTrades
	stats.LosingTrades = losingTrades
	stats.TotalPnL = fmt.Sprintf("%.4f", totalPnL)
	if totalVolume > 0 {
		stats.TotalPnLPercent = fmt.Sprintf("%.4f", (totalPnL/totalVolume)*100)
	} else {
		stats.TotalPnLPercent = "0.0000"
	}
}

func generateRunID() string {
	return time.Now().Format("20060102_150405")
}

// getGridPositionsForSave extracts grid positions for saving (avoids import cycle)
func getGridPositionsForSave() map[string]any {
	if GridBot == nil {
		return nil
	}
	GridBot.Mu().Lock()
	defer GridBot.Mu().Unlock()

	result := make(map[string]any)
	for symbol, pos := range GridBot.Positions {
		if pos.Status == "OPEN" {
			result[symbol] = &trading.GridPositionData{
				Symbol:       pos.Symbol,
				BaseAsset:    pos.BaseAsset,
				QuoteAsset:   pos.QuoteAsset,
				EntryPrice:   pos.EntryPrice,
				Quantity:     pos.Quantity,
				EntryTime:    pos.EntryTime,
				TargetProfit: pos.TargetProfit,
				StopLoss:     pos.StopLoss,
				Status:       pos.Status,
				MaxHoldTime:  pos.MaxHoldTime,
			}
		}
	}
	return result
}

// restoreGridPositions restores grid positions from loaded data
func restoreGridPositions(gridPositionsMap map[string]any) {
	if GridBot == nil || len(gridPositionsMap) == 0 {
		return
	}

	GridBot.Mu().Lock()
	defer GridBot.Mu().Unlock()

	for symbol, posInterface := range gridPositionsMap {
		if posData, ok := posInterface.(*trading.GridPositionData); ok && posData.Status == "OPEN" {
			GridBot.Positions[symbol] = &grid.Position{
				Symbol:       posData.Symbol,
				BaseAsset:    posData.BaseAsset,
				QuoteAsset:   posData.QuoteAsset,
				EntryPrice:   posData.EntryPrice,
				Quantity:     posData.Quantity,
				EntryTime:    posData.EntryTime,
				TargetProfit: posData.TargetProfit,
				StopLoss:     posData.StopLoss,
				Status:       posData.Status,
				MaxHoldTime:  posData.MaxHoldTime,
			}
		}
	}
}

// startGridTrading initializes and starts the grid trading bot
func startGridTrading(ob *arbitrage.Orderbook) {
	// Create grid trading bot
	GridBot = grid.NewGridTradingBot(cfg, client, ob, &SimTradeLogger)

	// Load saved positions after bot initialization
	gridPositionsMap := make(map[string]interface{})
	if err := trading.LoadPositions(TradingBot, MarginBot, FuturesBot, gridPositionsMap); err != nil {
		logger.WarningFmt("Failed to load saved positions: %v", err)
	}

	// Restore saved grid positions
	restoreGridPositions(gridPositionsMap)

	_, err := CheckAccountBalance()
	if err != nil {
		logger.Error(err)
		return
	}

	// Start grid trading
	err = GridBot.StartGridTrading()
	if err != nil {
		logger.ErrorFmt("Failed to start grid trading: %v", err)
		return
	}

	// Set up websocket for real-time price updates
	symbols := make([]string, 0, ob.Symbols.Count())
	for symbol := range ob.Symbols.IterBuffered() {
		symbols = append(symbols, symbol.Key)
	}

	websocketStreamClient := binance.NewWebsocketStreamClient(true)
	handler := NewBookTickerHandler(ob)

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
	logger.InfoFmt("%s", logger.Reset()+logger.BrightYellow(logger.Italic("Grid trading bot is running...")))

	// Start periodic position saving
	positionSaveTicker := time.NewTicker(30 * time.Second)
	positionSaveStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-positionSaveTicker.C:
				if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
					logger.WarningFmt("Failed to save positions periodically: %v", err)
				}
			case <-positionSaveStop:
				return
			}
		}
	}()

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	// Wait for either an error or an interrupt signal
	select {
	case err := <-errCh:
		logger.Error(err)
		close(stopCh) // Signal all goroutines to stop
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("grid")
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing connections...")
		close(stopCh) // Signal all goroutines to stop
		positionSaveTicker.Stop()
		close(positionSaveStop)
		// Save positions before shutdown
		if err := trading.SavePositions(TradingBot, MarginBot, FuturesBot, getGridPositionsForSave(), positionsLoaded); err != nil {
			logger.ErrorFmt("Failed to save positions: %v", err)
		}
		// Save statistics
		saveRunStatistics("grid")
	}

	wg.Wait()
}

// startTriangleArbitrage starts the original triangle arbitrage mode
func startTriangleArbitrage() {
	triangles := OrderBook.FindTriangles()
	logger.InfoFmt("%s", logger.Green(fmt.Sprintf("Found %d triangles!", len(triangles))))

	if !cfg.GeneralConfig.WdEnabled {
		logger.ErrorFmt("%s", logger.Red("[Binance error] Enable withdrawals for this token to continue with the current action."))
		logger.WarningFmt("%s", logger.Yellow("Starting on August 25th tokens will be required to have withdrawal permission in order to trade all USDC or USDT pairs."))
		return
	}

	_, err := CheckAccountBalance()
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
		// Save statistics
		saveRunStatistics("triangle")
	case <-quitChannel:
		logger.InfoFmt("Received interrupt signal. Closing connections...")
		close(stopCh) // Signal all goroutines to stop
		// Save statistics
		saveRunStatistics("triangle")
	}

	wg.Wait()
}

func loop(triangles []*arbitrage.Triangle, ob *arbitrage.Orderbook) {
	for {
		opportunities := make([]*arbitrage.Triangle, 0)
		for _, triangle := range triangles {
			found, profit, err := triangle.CheckArbitrage(ob, cfg.GeneralConfig.FeeRate)
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
		if !cfg.GeneralConfig.SimulationMode {
			count := 0

			for _, t := range opportunities {
				if count < 5 {
					locked, err := t.Execute(ob, cfg.TriangleConfig.OrderUSDCAmount)
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
