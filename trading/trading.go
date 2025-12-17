package trading

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog"
)

type TradingBot struct {
	ob              *arbitrage.Orderbook
	cfg             *utils.Config
	tradeLogger     *zerolog.Logger
	TrackedSymbols  []currency.Pair
	Positions       map[string]*Position
	ClosedPositions map[string][]ClosedPosition
	History         map[string][]PricePoint
	UpwardMovements map[string]*UpwardMovement
	mu              sync.RWMutex
}

type UpwardMovement struct {
	Symbol      currency.Pair
	LowPrice    string
	HighPrice   string
	Timestamp   time.Time
	RoundsInRow int
	IsUpward    bool
}

type PricePoint struct {
	BidPrice  string
	AskPrice  string
	Timestamp time.Time
}

type Position struct {
	Symbol           currency.Pair
	EntryPrice       string
	Quantity         string
	EntryTime        time.Time
	ProfitPercentage float64
	ProfitAmount     float64
	Status           string // "OPEN", "CLOSED", "STOPPED"
	MaxHoldTime      time.Duration
}

type ClosedPosition struct {
	EntryPosition Position
	ClosingPrice  string
	ClosingTime   time.Time
	Profit        string
}

func NewTradingBot(ob *arbitrage.Orderbook, tradeLogger *zerolog.Logger) *TradingBot {
	closedPositionsMap := make(map[string][]ClosedPosition)
	trackedSymbols := make([]currency.Pair, 0)
	history := make(map[string][]PricePoint)
	upwardMovements := make(map[string]*UpwardMovement)

	for _, coin := range ob.Config.NormalConfig.QuoteAssets {
		pair := currency.Pair{
			Base:  currency.Currency{Symbol: coin},
			Quote: currency.Currency{Symbol: ob.Config.NormalConfig.BaseAsset},
		}
		closedPositionsArray := make([]ClosedPosition, 0)
		closedPositionsMap[coin+ob.Config.NormalConfig.BaseAsset] = closedPositionsArray
		trackedSymbols = append(trackedSymbols, pair)
		history[coin+ob.Config.NormalConfig.BaseAsset] = make([]PricePoint, 0)
		upwardMovements[coin+ob.Config.NormalConfig.BaseAsset] = &UpwardMovement{
			Symbol:      pair,
			RoundsInRow: 0,
			IsUpward:    false,
		}
	}

	return &TradingBot{
		cfg:             ob.Config,
		ob:              ob,
		tradeLogger:     tradeLogger,
		TrackedSymbols:  trackedSymbols,
		Positions:       make(map[string]*Position),
		ClosedPositions: closedPositionsMap,
		History:         history,
		UpwardMovements: upwardMovements,
	}
}

// Mu returns the mutex (for thread-safe access)
func (tb *TradingBot) Mu() *sync.RWMutex {
	return &tb.mu
}

func (tb *TradingBot) StartTrading() {
	go tb.tradingLoop()
}

func (tb *TradingBot) tradingLoop() {
	ticker := time.NewTicker(time.Second * 5)
	defer ticker.Stop()

	for range ticker.C {
		tb.processTradingCycle()
	}
}

func (tb *TradingBot) processTradingCycle() {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	tb.updatePriceHistory()

	tb.checkExistingPositions()
	tb.findNewOpportunities()
}

func (tb *TradingBot) checkExistingPositions() {
	logger.InfoFmt("Checking %d existing positions", len(tb.Positions))
	for symbolStr, position := range tb.Positions {
		if position.Status != "OPEN" {
			continue
		}

		if tb.cfg.GeneralConfig.VerboseLogging {
			logger.InfoFmt("Checking position %s", symbolStr)
		}

		// Get current price for the symbol
		symbolData, ok := tb.ob.Symbols.Get(symbolStr)
		if !ok {
			logger.WarningFmt("Symbol %s not found in orderbook for position check", symbolStr)
			continue
		}

		bookTicker := symbolData.GetBookTicker()
		if bookTicker == nil {
			continue
		}

		// Calculate current profit/loss using bid price (what we'd get when selling)
		currentPrice, err := strconv.ParseFloat(bookTicker.BidPrice, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse current price for %s: %v", symbolStr, err)
			continue
		}

		entryPrice, err := strconv.ParseFloat(position.EntryPrice, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse entry price for %s: %v", symbolStr, err)
			continue
		}

		// Calculate bid-ask spread for debugging
		askPrice, err := strconv.ParseFloat(bookTicker.AskPrice, 64)
		if err == nil {
			spread := ((askPrice - currentPrice) / currentPrice) * 100
			if tb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Bid-Ask spread for %s: %.4f%% (Bid: %.8f, Ask: %.8f)",
					symbolStr, spread, currentPrice, askPrice)
			}
		}

		quantity, err := strconv.ParseFloat(position.Quantity, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse quantity for %s: %v", symbolStr, err)
			continue
		}

		// Calculate profit/loss based on total USDC amounts
		totalUSDCReceived := quantity * currentPrice
		totalUSDCSpent := quantity * entryPrice
		profitAmount := totalUSDCReceived - totalUSDCSpent

		// Calculate profit percentage
		profitPercentage := profitAmount / totalUSDCSpent

		// Debug logging for profit calculation
		if tb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Profit calculation for %s: Qty=%.8f, Entry=%.8f, Current=%.8f, Spent=%.2f, Received=%.2f, Profit=%.2f, Pct=%.4f%%",
				symbolStr, quantity, entryPrice, currentPrice, totalUSDCSpent, totalUSDCReceived, profitAmount, profitPercentage*100)
		}

		// Update position profit
		position.ProfitPercentage = profitPercentage
		position.ProfitAmount = profitAmount

		// Log the calculation
		if tb.cfg.GeneralConfig.VerboseLogging {
			logger.InfoFmt("Position %s: Entry=%.8f, Current=%.8f, P&L=%.4f%% (%.2f USDC), Target=%.4f%%, Stop=%.4f%%",
				symbolStr, entryPrice, currentPrice, profitPercentage*100, profitAmount,
				tb.cfg.NormalConfig.TargetProfit*100, tb.cfg.NormalConfig.StopLoss*100)
		}

		// Check for profit target or stop loss
		if profitPercentage >= tb.cfg.NormalConfig.TargetProfit {
			logger.InfoFmt("Profit target reached for %s: %.4f%% >= %.4f%%",
				symbolStr, profitPercentage*100, tb.cfg.NormalConfig.TargetProfit*100)
			tb.closePosition(symbolStr, "PROFIT_TARGET")
		} else if profitPercentage <= -tb.cfg.NormalConfig.StopLoss {
			logger.InfoFmt("Stop loss triggered for %s: %.4f%% <= -%.4f%%",
				symbolStr, profitPercentage*100, tb.cfg.NormalConfig.StopLoss*100)
			tb.closePosition(symbolStr, "STOP_LOSS")
		}

		// Check if position has exceeded max hold time
		// Only close on timeout if position is profitable (P&L > 0)
		// If losing money, keep it open unless stop loss is reached
		if time.Since(position.EntryTime) > position.MaxHoldTime {
			if profitAmount > 0 {
				logger.InfoFmt("Max hold time reached for %s with positive P&L (%.2f USDC). Closing position.", symbolStr, profitAmount)
				tb.closePosition(symbolStr, "TIMEOUT")
			} else {
				logger.InfoFmt("Max hold time reached for %s but position is unprofitable (P&L=%.2f USDC). Keeping position open until stop loss.", symbolStr, profitAmount)
			}
			continue
		}
	}
}

func (tb *TradingBot) updatePriceHistory() {
	for _, symbol := range tb.TrackedSymbols {
		symbolData, ok := tb.ob.Symbols.Get(symbol.String())
		if !ok {
			continue
		}

		bookTicker := symbolData.GetBookTicker()
		if bookTicker == nil {
			continue
		}

		tb.History[symbol.String()] = append(tb.History[symbol.String()], PricePoint{
			BidPrice:  bookTicker.BidPrice,
			AskPrice:  bookTicker.AskPrice,
			Timestamp: time.Now(),
		})

		// Keep only last 50 price points to avoid memory issues
		if len(tb.History[symbol.String()]) > 50 {
			tb.History[symbol.String()] = tb.History[symbol.String()][1:]
		}

		if tb.cfg.GeneralConfig.TraceLogging {
			logger.TraceFmt("Updated price history for %s: %v", symbol.String(), tb.History[symbol.String()])
		}
	}

	if tb.cfg.GeneralConfig.TraceLogging {
		logger.DebugFmt("Updated price history for %d symbols", len(tb.History))
	}
}

func (tb *TradingBot) findNewOpportunities() {
	for _, symbol := range tb.TrackedSymbols {
		// Skip if we already have a position for this symbol
		if _, hasPosition := tb.Positions[symbol.String()]; hasPosition {
			continue
		}

		// Check if we've reached max positions
		if len(tb.Positions) >= tb.cfg.NormalConfig.MaxPositions {
			break
		}

		history, ok := tb.History[symbol.String()]
		if !ok || len(history) < 10 { // Need more history for better trend analysis
			continue
		}

		// Analyze price movement over the last 10 data points
		priceChange, isUpwardTrend := tb.analyzePriceMovement(history)

		if isUpwardTrend && priceChange >= tb.cfg.NormalConfig.MinProfit {
			// Significant upward movement detected, open position
			tb.openPosition(symbol, history[len(history)-1])
		}
	}
}

// analyzePriceMovement analyzes the price history to detect upward trends
// Returns the total price change percentage and whether it's an upward trend
func (tb *TradingBot) analyzePriceMovement(history []PricePoint) (float64, bool) {
	if len(history) < 2 {
		return 0, false
	}

	// Get the first and last prices for overall trend
	firstPrice, err := strconv.ParseFloat(history[0].AskPrice, 64)
	if err != nil {
		return 0, false
	}

	lastPrice, err := strconv.ParseFloat(history[len(history)-1].AskPrice, 64)
	if err != nil {
		return 0, false
	}

	// Calculate overall price change percentage
	priceChange := (lastPrice - firstPrice) / firstPrice

	// Check for consistent upward movement by analyzing recent data points
	upwardCount := 0
	recentDataPoints := 5 // Check last 5 data points
	startIndex := max(len(history)-recentDataPoints, 0)

	for i := startIndex; i < len(history)-1; i++ {
		currentPrice, err1 := strconv.ParseFloat(history[i].AskPrice, 64)
		nextPrice, err2 := strconv.ParseFloat(history[i+1].AskPrice, 64)

		if err1 != nil || err2 != nil {
			continue
		}

		if nextPrice > currentPrice {
			upwardCount++
		}
	}

	// Consider it an upward trend if at least 60% of recent movements are upward
	// and the overall price change is positive
	isUpwardTrend := upwardCount >= int(float64(recentDataPoints-1)*0.6) && priceChange > 0

	if tb.cfg.GeneralConfig.TraceLogging {
		logger.DebugFmt("Price analysis: change=%.4f%%, upward_movements=%d/%d, is_upward=%t",
			priceChange*100, upwardCount, recentDataPoints-1, isUpwardTrend)
	}

	return priceChange, isUpwardTrend
}

// openPosition opens a new trading position for the given symbol
func (tb *TradingBot) openPosition(symbol currency.Pair, pricePoint PricePoint) {
	entryPrice, err := strconv.ParseFloat(pricePoint.AskPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for %s: %v", symbol.String(), err)
		return
	}

	symbolData, ok := tb.ob.Symbols.Get(symbol.String())
	if !ok {
		logger.ErrorFmt("Symbol %s not found in orderbook for position open", symbol.String())
		return
	}

	// Calculate quantity based on USDC amount
	quantity := tb.cfg.NormalConfig.USDCAmount / entryPrice
	quantity, err = arbitrage.StepSizeQuantityFloat(symbolData, quantity)
	if err != nil {
		logger.ErrorFmt("Failed to apply step size to quantity for %s: %v", symbol.String(), err)
		return
	}

	// Use the actual market ask price as entry price
	entryPricePerUnit, err := strconv.ParseFloat(pricePoint.AskPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for %s: %v", symbol.String(), err)
		return
	}

	position := &Position{
		Symbol:           symbol,
		EntryPrice:       strconv.FormatFloat(entryPricePerUnit, 'f', 8, 64),
		Quantity:         strconv.FormatFloat(quantity, 'f', int(symbolData.BasePrecision), 64),
		EntryTime:        time.Now(),
		ProfitPercentage: 0,
		ProfitAmount:     0,
		Status:           "OPEN",
		MaxHoldTime:      time.Duration(tb.cfg.NormalConfig.MaxHoldTime) * time.Minute,
	}

	cummulativeQuoteQty := tb.cfg.NormalConfig.USDCAmount

	if !tb.cfg.GeneralConfig.SimulationMode {
		res, err := tb.NewMarketOrder(&symbol, "BUY", quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute buy order for %s: %v", symbol.String(), err)
			return
		}
		position.Quantity = res.ExecutedQty

		// Calculate the actual entry price per unit from the executed trade
		executedQty, err := strconv.ParseFloat(res.ExecutedQty, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse executed quantity for %s: %v", symbol.String(), err)
			return
		}
		cummulativeQuoteQty, err = strconv.ParseFloat(res.CummulativeQuoteQty, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse cummulative quote quantity for %s: %v", symbol.String(), err)
			return
		}
		actualEntryPricePerUnit := cummulativeQuoteQty / executedQty
		position.EntryPrice = strconv.FormatFloat(actualEntryPricePerUnit, 'f', 8, 64)

		assets := make([]string, 0)
		assets = append(assets, tb.cfg.NormalConfig.QuoteAssets...)
		assets = append(assets, tb.cfg.NormalConfig.BaseAsset)

		_, err = utils.CheckAccountBalances(tb.ob.Client, assets)
		if err != nil {
			logger.ErrorFmt("Failed to check account balances for %s: %v", symbol.String(), err)
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would execute buy order for %s: %v", symbol.String(), quantity)))
	}

	tb.Positions[symbol.String()] = position

	// Update upward movement tracking
	if movement, exists := tb.UpwardMovements[symbol.String()]; exists {
		movement.HighPrice = pricePoint.AskPrice
		movement.Timestamp = time.Now()
		movement.RoundsInRow++
		movement.IsUpward = true
	}

	logger.InfoFmt("Opened position for %s: Entry=%.8f, Quantity=%.8f, USDC=%.2f",
		symbol.String(), entryPricePerUnit, quantity, cummulativeQuoteQty)

	// Log to trade logger
	tb.tradeLogger.Info().
		Str("action", "OPEN_POSITION").
		Str("symbol", symbol.String()).
		Str("entry_price", pricePoint.AskPrice).
		Str("quantity", position.Quantity).
		Float64("usdc_amount", tb.cfg.NormalConfig.USDCAmount).
		Time("entry_time", position.EntryTime).
		Msg("Position opened")
}

// closePosition closes an existing trading position
func (tb *TradingBot) closePosition(symbolStr, reason string) {
	position, exists := tb.Positions[symbolStr]
	if !exists || position.Status != "OPEN" {
		return
	}

	// Get current price for closing
	symbolData, ok := tb.ob.Symbols.Get(symbolStr)
	if !ok {
		logger.WarningFmt("Symbol %s not found in orderbook for position close", symbolStr)
		return
	}

	bookTicker := symbolData.GetBookTicker()
	if bookTicker == nil {
		logger.WarningFmt("No book ticker data for %s during position close", symbolStr)
		return
	}

	// Calculate final profit/loss
	exitPricePerUnit, err := strconv.ParseFloat(bookTicker.BidPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse current price for closing %s: %v", symbolStr, err)
		return
	}

	entryPrice, err := strconv.ParseFloat(position.EntryPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for closing %s: %v", symbolStr, err)
		return
	}

	// Calculate the actual USDC amount we would receive when selling
	quantity, err := strconv.ParseFloat(position.Quantity, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse quantity for closing %s: %v", symbolStr, err)
		return
	}

	// Calculate total USDC we'd receive
	totalUSDCReceived := quantity * exitPricePerUnit
	// Calculate total USDC we spent
	totalUSDCSpent := quantity * entryPrice

	profitAmount := totalUSDCReceived - totalUSDCSpent
	profitPercentage := profitAmount / totalUSDCSpent

	// Update position status
	position.Status = "CLOSED"
	position.ProfitPercentage = profitPercentage
	position.ProfitAmount = profitAmount

	// Create closed position record
	closedPosition := ClosedPosition{
		EntryPosition: *position,
		ClosingPrice:  bookTicker.BidPrice,
		ClosingTime:   time.Now(),
		Profit:        strconv.FormatFloat(profitAmount, 'f', 6, 64),
	}

	quantity, err = strconv.ParseFloat(position.Quantity, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse quantity for %s: %v", symbolStr, err)
		return
	}

	if !tb.cfg.GeneralConfig.SimulationMode {
		res, err := tb.NewMarketOrder(&symbolData.Pair, "SELL", quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute sell order for %s: %v", symbolStr, err)
			return
		}
		position.Quantity = res.ExecutedQty

		// Calculate the actual exit price per unit from the executed trade
		executedQty, err := strconv.ParseFloat(res.ExecutedQty, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse executed quantity for %s: %v", symbolStr, err)
			return
		}
		cummulativeQuoteQty, err := strconv.ParseFloat(res.CummulativeQuoteQty, 64)
		if err != nil {
			logger.ErrorFmt("Failed to parse cummulative quote quantity for %s: %v", symbolStr, err)
			return
		}
		actualExitPricePerUnit := cummulativeQuoteQty / executedQty
		closedPosition.ClosingPrice = strconv.FormatFloat(actualExitPricePerUnit, 'f', 8, 64)

		// Calculate real profit using the actual prices
		realProfit := (actualExitPricePerUnit - entryPrice) * executedQty
		closedPosition.Profit = strconv.FormatFloat(realProfit, 'f', 6, 64)

		assets := make([]string, 0)
		assets = append(assets, tb.cfg.NormalConfig.QuoteAssets...)
		assets = append(assets, tb.cfg.NormalConfig.BaseAsset)

		_, err = utils.CheckAccountBalances(tb.ob.Client, assets)
		if err != nil {
			logger.ErrorFmt("Failed to check account balances for %s: %v", symbolStr, err)
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would execute sell order for %s: %v", symbolStr, position.Quantity)))
	}

	// Add to closed positions history
	tb.ClosedPositions[symbolStr] = append(tb.ClosedPositions[symbolStr], closedPosition)

	// Remove from active positions
	delete(tb.Positions, symbolStr)

	// Log the closure
	logger.InfoFmt("Closed position for %s: Entry=%.8f, Exit=%.8f, P&L=%.4f%% (%.2f USDC), Reason=%s",
		symbolStr, entryPrice, exitPricePerUnit, profitPercentage*100, profitAmount, logger.ColorizeReason(reason))

	// Log to trade logger
	tb.tradeLogger.Info().
		Str("action", "CLOSE_POSITION").
		Str("symbol", symbolStr).
		Str("entry_price", position.EntryPrice).
		Str("closing_price", bookTicker.BidPrice).
		Str("profit_percentage", closedPosition.Profit).
		Float64("profit_amount", profitAmount).
		Str("reason", reason).
		Time("closing_time", closedPosition.ClosingTime).
		Msg("Position closed")

	// Update upward movement tracking
	if movement, exists := tb.UpwardMovements[symbolStr]; exists {
		movement.IsUpward = false
		movement.RoundsInRow = 0
	}
}

func (tb *TradingBot) NewMarketOrder(pair *currency.Pair, dir string, qty float64) (*binance.CreateOrderResponseFULL, error) {
	symbol := pair.String()

	// Get symbol data to apply step size validation
	symbolData, ok := tb.ob.Symbols.Get(symbol)
	if !ok {
		return nil, fmt.Errorf("symbol %s not found in orderbook", symbol)
	}

	// Apply step size validation
	validatedQty, err := arbitrage.StepSizeQuantity(symbolData, udecimal.MustFromFloat64(qty))
	if err != nil {
		return nil, fmt.Errorf("failed to apply step size to quantity: %v", err)
	}

	// Format quantity with correct precision for the API
	quantityStr := validatedQty.StringFixed(uint8(symbolData.BasePrecision))
	formattedQty, err := strconv.ParseFloat(quantityStr, 64)
	if err != nil {
		return nil, fmt.Errorf("failed to format quantity with correct precision: %v", err)
	}

	order, err := tb.ob.Client.NewCreateOrderService().Symbol(symbol).
		Quantity(formattedQty).Type("MARKET").Side(dir).Do(context.Background())
	if err != nil {
		return nil, err
	}

	var color func(msg string) string
	if dir == "BUY" {
		color = logger.Green
	} else {
		color = logger.Red
	}

	res := order.(*binance.CreateOrderResponseFULL)

	if tb.cfg.GeneralConfig.VerboseLogging {
		logger.Info(binance.PrettyPrint(res))
	}
	logger.InfoFmt("Order for %s placed. %s %s %s for %s %s at price %s USDC", logger.Cyan(symbol), color(dir), res.ExecutedQty, pair.Base, res.CummulativeQuoteQty, pair.Quote, res.Fills[0].Price)

	return res, nil
}
