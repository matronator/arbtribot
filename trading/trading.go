package trading

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"fmt"
	"strconv"
	"sync"
	"time"

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

func (tb *TradingBot) StartTrading() {
	go tb.tradingLoop()
}

func (tb *TradingBot) tradingLoop() {
	ticker := time.NewTicker(5 * time.Second)
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
	for symbolStr, position := range tb.Positions {
		if position.Status != "OPEN" {
			continue
		}

		// Check if position has exceeded max hold time
		if time.Since(position.EntryTime) > position.MaxHoldTime {
			tb.closePosition(symbolStr, "TIMEOUT")
			continue
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

		// Calculate current profit/loss
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

		profitAmount := currentPrice - entryPrice

		// Calculate profit percentage
		profitPercentage := profitAmount / entryPrice

		// Update position profit
		position.ProfitPercentage = profitPercentage
		position.ProfitAmount = profitAmount

		// Check for profit target or stop loss
		if profitPercentage >= tb.cfg.NormalConfig.TargetProfit {
			tb.closePosition(symbolStr, "PROFIT_TARGET")
		} else if profitPercentage <= -tb.cfg.NormalConfig.StopLoss {
			tb.closePosition(symbolStr, "STOP_LOSS")
		}

		// Log position status
		if tb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Position %s: Entry=%.8f, Current=%.8f, P&L=%.4f%% (%.2f USDC)",
				symbolStr, entryPrice, currentPrice, profitPercentage*100, profitAmount)
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

	if tb.cfg.GeneralConfig.VerboseLogging {
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

	position := &Position{
		Symbol:           symbol,
		EntryPrice:       pricePoint.AskPrice,
		Quantity:         strconv.FormatFloat(quantity, 'f', int(symbolData.BasePrecision), 64),
		EntryTime:        time.Now(),
		ProfitPercentage: 0,
		ProfitAmount:     0,
		Status:           "OPEN",
		MaxHoldTime:      time.Duration(tb.cfg.NormalConfig.MaxHoldTime) * time.Minute,
	}

	if !tb.cfg.GeneralConfig.SimulationMode {
		res, err := arbitrage.NewMarketOrder(tb.ob.Client, &symbol, "BUY", quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute buy order for %s: %v", symbol.String(), err)
			return
		}
		position.Quantity = res.ExecutedQty
		position.EntryPrice = res.CummulativeQuoteQty
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
		symbol.String(), entryPrice, quantity, tb.cfg.NormalConfig.USDCAmount)

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
	currentPrice, err := strconv.ParseFloat(bookTicker.BidPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse current price for closing %s: %v", symbolStr, err)
		return
	}

	entryPrice, err := strconv.ParseFloat(position.EntryPrice, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for closing %s: %v", symbolStr, err)
		return
	}

	profitAmount := currentPrice - entryPrice
	profitPercentage := profitAmount / entryPrice

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

	quantity, err := strconv.ParseFloat(position.Quantity, 64)
	if err != nil {
		logger.ErrorFmt("Failed to parse quantity for %s: %v", symbolStr, err)
		return
	}

	if !tb.cfg.GeneralConfig.SimulationMode {
		_, err = arbitrage.NewMarketOrder(tb.ob.Client, &symbolData.Pair, "SELL", quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute sell order for %s: %v", symbolStr, err)
			return
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
		symbolStr, entryPrice, currentPrice, profitPercentage*100, profitAmount, reason)

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
