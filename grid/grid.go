package grid

import (
	"arbtribot/arbitrage"
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog"
)

// GridTradingBot manages grid-based trading operations
type GridTradingBot struct {
	Config        *utils.Config
	Client        *binance.Client
	OrderBook     *arbitrage.Orderbook
	Positions     map[string]*Position
	PriceHistory  map[string][]PricePoint
	mu            sync.RWMutex
	TradeLogger   *zerolog.Logger
	ActiveSymbols []string
}

// parsePairFromSymbol parses a symbol string into a currency.Pair
func parsePairFromSymbol(symbolStr string) (currency.Pair, error) {
	// Check if the symbol exists in AllSymbols
	if pair, exists := currency.AllSymbols[symbolStr]; exists {
		return pair, nil
	}

	// If not found, try to parse it manually
	// This is a fallback for symbols not in the predefined list
	// We'll assume it's a USDC pair for now
	if strings.HasSuffix(symbolStr, "USDC") {
		baseSymbol := strings.TrimSuffix(symbolStr, "USDC")
		return currency.Pair{
			Base:  currency.Currency{Symbol: baseSymbol},
			Quote: currency.USDC,
		}, nil
	}

	return currency.Pair{}, fmt.Errorf("unable to parse symbol: %s", symbolStr)
}

// Position represents an active trading position
type Position struct {
	Symbol       string
	BaseAsset    string
	QuoteAsset   string
	EntryPrice   float64
	Quantity     float64
	EntryTime    time.Time
	TargetProfit float64 // Target profit percentage (e.g., 0.02 for 2%)
	StopLoss     float64 // Stop loss percentage (e.g., 0.01 for 1%)
	Status       string  // "OPEN", "CLOSED", "STOPPED"
	MaxHoldTime  time.Duration
}

// PricePoint represents a price at a specific time
type PricePoint struct {
	Price     float64
	Timestamp time.Time
}

// GridConfig holds configuration for grid trading
type GridConfig struct {
	MinPriceChange float64 // Minimum price change to consider a trend (e.g., 0.005 for 0.5%)
	ProfitTarget   float64 // Target profit percentage (e.g., 0.02 for 2%)
	StopLoss       float64 // Stop loss percentage (e.g., 0.01 for 1%)
	MaxHoldTime    int     // Maximum hold time in minutes
	MinVolume      float64 // Minimum 24h volume to consider a symbol
	MaxPositions   int     // Maximum number of concurrent positions
	CheckInterval  int     // Price check interval in seconds
	TrendWindow    int     // Number of price points to consider for trend analysis
}

// NewGridTradingBot creates a new grid trading bot instance
func NewGridTradingBot(config *utils.Config, client *binance.Client, orderBook *arbitrage.Orderbook, tradeLogger *zerolog.Logger) *GridTradingBot {
	return &GridTradingBot{
		Config:        config,
		Client:        client,
		OrderBook:     orderBook,
		Positions:     make(map[string]*Position),
		PriceHistory:  make(map[string][]PricePoint),
		TradeLogger:   tradeLogger,
		ActiveSymbols: []string{},
	}
}

// Mu returns the mutex (for thread-safe access)
func (gtb *GridTradingBot) Mu() *sync.RWMutex {
	return &gtb.mu
}

// GetDefaultGridConfig returns default configuration for grid trading
func GetDefaultGridConfig() *GridConfig {
	return &GridConfig{
		MinPriceChange: 0.005,  // 0.5% minimum price change
		ProfitTarget:   0.02,   // 2% profit target
		StopLoss:       0.01,   // 1% stop loss
		MaxHoldTime:    30,     // 30 minutes max hold time
		MinVolume:      100000, // $100k minimum 24h volume
		MaxPositions:   5,      // Maximum 5 concurrent positions
		CheckInterval:  10,     // Check every 10 seconds
		TrendWindow:    5,      // Use last 5 price points for trend analysis
	}
}

// StartGridTrading begins the grid trading process
func (gtb *GridTradingBot) StartGridTrading() error {
	gridConfig := &GridConfig{
		MinPriceChange: gtb.Config.GridConfig.GridMinPriceChange,
		ProfitTarget:   gtb.Config.GridConfig.GridProfitTarget,
		StopLoss:       gtb.Config.GridConfig.GridStopLoss,
		MaxHoldTime:    gtb.Config.GridConfig.GridMaxHoldTime,
		MaxPositions:   gtb.Config.GridConfig.GridMaxPositions,
		CheckInterval:  gtb.Config.GridConfig.GridCheckInterval,
		MinVolume:      100000, // Default minimum volume
		TrendWindow:    5,      // Default trend window
	}

	logger.InfoFmt("%s", logger.Green("Starting Grid Trading Bot..."))
	logger.InfoFmt("Grid Config: MinChange=%.2f%%, ProfitTarget=%.2f%%, StopLoss=%.2f%%, MaxPositions=%d",
		gridConfig.MinPriceChange*100, gridConfig.ProfitTarget*100, gridConfig.StopLoss*100, gridConfig.MaxPositions)

	// Get all available symbols
	symbols := gtb.getAvailableSymbols()
	if len(symbols) == 0 {
		return fmt.Errorf("no symbols available for trading")
	}

	logger.InfoFmt("Monitoring %d symbols for trading opportunities", len(symbols))
	for _, symbol := range symbols {
		logger.DebugFmt("Monitoring symbol: %s", symbol)
	}
	gtb.ActiveSymbols = symbols

	// Start the main trading loop
	go gtb.tradingLoop(gridConfig)

	return nil
}

// getAvailableSymbols returns a list of symbols suitable for grid trading
func (gtb *GridTradingBot) getAvailableSymbols() []string {
	var symbols []string

	// Get symbols from orderbook that have USDC as quote asset
	for symbol := range gtb.OrderBook.Symbols.IterBuffered() {
		symbolStr := symbol.Key
		pair, err := parsePairFromSymbol(symbolStr)
		if err != nil {
			continue
		}

		// Only consider USDC pairs for simplicity
		if pair.Quote.String() == "USDC" {
			symbols = append(symbols, symbolStr)
		}
	}

	// Limit to top 50 most liquid symbols for now
	if len(symbols) > 50 {
		symbols = symbols[:50]
	}

	return symbols
}

// tradingLoop is the main trading loop that monitors prices and manages positions
func (gtb *GridTradingBot) tradingLoop(config *GridConfig) {
	ticker := time.NewTicker(time.Duration(config.CheckInterval) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		gtb.processTradingCycle(config)
	}
}

// processTradingCycle processes one trading cycle
func (gtb *GridTradingBot) processTradingCycle(config *GridConfig) {
	gtb.mu.Lock()
	defer gtb.mu.Unlock()

	// Update price history for all active symbols
	gtb.updatePriceHistory()

	// Check existing positions for profit/loss
	gtb.checkExistingPositions()

	// Look for new trading opportunities if we have capacity
	if len(gtb.Positions) < config.MaxPositions {
		gtb.findNewOpportunities(config)
	}

	// Log current status
	gtb.logStatus()
}

// updatePriceHistory updates price history for all active symbols
func (gtb *GridTradingBot) updatePriceHistory() {
	for _, symbolStr := range gtb.ActiveSymbols {
		symbol, ok := gtb.OrderBook.Symbols.Get(symbolStr)
		if !ok {
			continue
		}

		bookTicker := symbol.GetBookTicker()
		if bookTicker == nil {
			continue
		}

		// Use Ask price for LONG positions (grid trading only uses LONG positions)
		askPrice, err := strconv.ParseFloat(bookTicker.AskPrice, 64)
		if err != nil {
			continue
		}

		// Add to price history
		pricePoint := PricePoint{
			Price:     askPrice,
			Timestamp: time.Now(),
		}

		gtb.PriceHistory[symbolStr] = append(gtb.PriceHistory[symbolStr], pricePoint)

		// Keep only last 20 price points to avoid memory issues
		if len(gtb.PriceHistory[symbolStr]) > 20 {
			gtb.PriceHistory[symbolStr] = gtb.PriceHistory[symbolStr][1:]
		}
	}
}

// checkExistingPositions checks all existing positions for profit/loss targets
func (gtb *GridTradingBot) checkExistingPositions() {
	for symbol, position := range gtb.Positions {
		if position.Status != "OPEN" {
			continue
		}

		// Get current price
		currentPrice := gtb.getCurrentPrice(symbol)
		if currentPrice == 0 {
			continue
		}

		// Calculate profit/loss percentage
		profitLoss := (currentPrice - position.EntryPrice) / position.EntryPrice

		// Check for profit target
		if profitLoss >= position.TargetProfit {
			gtb.closePosition(symbol, "PROFIT", currentPrice, profitLoss)
			continue
		}

		// Check for stop loss
		if profitLoss <= -position.StopLoss {
			gtb.closePosition(symbol, "STOP_LOSS", currentPrice, profitLoss)
			continue
		}

		// Check for max hold time
		// Only close on timeout if position is profitable (P&L > 0)
		// If losing money, keep it open unless stop loss is reached
		if time.Since(position.EntryTime) > position.MaxHoldTime {
			if profitLoss > 0 {
				logger.InfoFmt("Max hold time reached for %s with positive P&L (%.2f%%). Closing position.", symbol, profitLoss*100)
				gtb.closePosition(symbol, "TIMEOUT", currentPrice, profitLoss)
			} else {
				logger.InfoFmt("Max hold time reached for %s but position is unprofitable (P&L=%.2f%%). Keeping position open until stop loss.", symbol, profitLoss*100)
			}
			continue
		}

		// Log position status
		if gtb.Config.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Position %s: Entry=%.6f, Current=%.6f, P/L=%.2f%%",
				symbol, position.EntryPrice, currentPrice, profitLoss*100)
		}
	}
}

// findNewOpportunities looks for new trading opportunities
func (gtb *GridTradingBot) findNewOpportunities(config *GridConfig) {
	for _, symbolStr := range gtb.ActiveSymbols {
		// Skip if we already have a position in this symbol
		if _, exists := gtb.Positions[symbolStr]; exists {
			continue
		}

		// Check if symbol has enough price history
		history := gtb.PriceHistory[symbolStr]
		if len(history) < config.TrendWindow {
			continue
		}

		// Analyze trend
		if gtb.isUptrend(history, config) {
			gtb.openPosition(symbolStr, config)
		}
	}
}

// isUptrend determines if the recent price history shows an uptrend
func (gtb *GridTradingBot) isUptrend(history []PricePoint, config *GridConfig) bool {
	if len(history) < config.TrendWindow {
		return false
	}

	// Get recent prices
	recent := history[len(history)-config.TrendWindow:]

	// Calculate price change
	oldestPrice := recent[0].Price
	newestPrice := recent[len(recent)-1].Price

	priceChange := (newestPrice - oldestPrice) / oldestPrice

	// Check if it's a significant uptrend
	return priceChange >= config.MinPriceChange
}

// openPosition opens a new trading position
func (gtb *GridTradingBot) openPosition(symbolStr string, config *GridConfig) {
	// Get entry price (Ask for LONG - buy at ask)
	entryPrice := gtb.getEntryPrice(symbolStr)
	if entryPrice == 0 {
		return
	}

	// Parse symbol to get base and quote assets
	pair, err := parsePairFromSymbol(symbolStr)
	if err != nil {
		logger.ErrorFmt("Failed to parse symbol %s: %v", symbolStr, err)
		return
	}

	// Calculate position size (use a small amount for safety)
	positionSize := gtb.Config.TriangleConfig.OrderUSDCAmount
	quantity := positionSize / entryPrice

	// Create position
	position := &Position{
		Symbol:       symbolStr,
		BaseAsset:    pair.Base.String(),
		QuoteAsset:   pair.Quote.String(),
		EntryPrice:   entryPrice,
		Quantity:     quantity,
		EntryTime:    time.Now(),
		TargetProfit: config.ProfitTarget,
		StopLoss:     config.StopLoss,
		Status:       "OPEN",
		MaxHoldTime:  time.Duration(config.MaxHoldTime) * time.Minute,
	}

	gtb.Positions[symbolStr] = position

	// Execute buy order if not in simulation mode
	if !gtb.Config.GeneralConfig.SimulationMode {
		err := gtb.executeBuyOrder(symbolStr, quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute buy order for %s: %v", symbolStr, err)
			delete(gtb.Positions, symbolStr)
			return
		}
	}

	logger.InfoFmt("%s Opened position: %s at %.6f USDC (Qty: %.6f)",
		logger.Green("BUY"), symbolStr, entryPrice, quantity)

	gtb.TradeLogger.Info().
		Str("action", "BUY").
		Str("symbol", symbolStr).
		Float64("price", entryPrice).
		Float64("quantity", quantity).
		Float64("amount", positionSize).
		Msg("Position opened")
}

// closePosition closes an existing position
func (gtb *GridTradingBot) closePosition(symbolStr string, reason string, currentPrice float64, profitLoss float64) {
	position, exists := gtb.Positions[symbolStr]
	if !exists {
		return
	}

	// Execute sell order if not in simulation mode
	if !gtb.Config.GeneralConfig.SimulationMode {
		err := gtb.executeSellOrder(symbolStr, position.Quantity)
		if err != nil {
			logger.ErrorFmt("Failed to execute sell order for %s: %v", symbolStr, err)
			return
		}
	}

	// Calculate profit/loss in USDC
	profitLossUSDC := profitLoss * gtb.Config.TriangleConfig.OrderUSDCAmount

	// Update position status
	position.Status = "CLOSED"

	// Log the trade
	color := logger.Red
	if profitLoss > 0 {
		color = logger.Green
	}

	logger.InfoFmt("%s Closed position: %s at %.6f USDC (P/L: %s / %s) - Reason: %s",
		color("SELL"), symbolStr, currentPrice,
		color(fmt.Sprintf("%+.2f%%", profitLoss*100)),
		color(fmt.Sprintf("%+.2f USDC", profitLossUSDC)),
		reason)

	gtb.TradeLogger.Info().
		Str("action", "SELL").
		Str("symbol", symbolStr).
		Float64("price", currentPrice).
		Float64("quantity", position.Quantity).
		Float64("profit_loss_pct", profitLoss*100).
		Float64("profit_loss_usdc", profitLossUSDC).
		Str("reason", reason).
		Msg("Position closed")

	// Remove from active positions
	delete(gtb.Positions, symbolStr)
}

// getEntryPrice gets the entry price for a LONG position (Ask price - buy at ask)
func (gtb *GridTradingBot) getEntryPrice(symbolStr string) float64 {
	symbol, ok := gtb.OrderBook.Symbols.Get(symbolStr)
	if !ok {
		return 0
	}

	bookTicker := symbol.GetBookTicker()
	if bookTicker == nil {
		return 0
	}

	// Use Ask price for LONG entry (buy at ask)
	askPrice, err := strconv.ParseFloat(bookTicker.AskPrice, 64)
	if err != nil {
		return 0
	}

	return askPrice
}

// getCurrentPrice gets the current exit price for PnL calculations (Bid price - sell at bid)
// Grid trading only uses LONG positions, so we use Bid for exit/PnL
func (gtb *GridTradingBot) getCurrentPrice(symbolStr string) float64 {
	symbol, ok := gtb.OrderBook.Symbols.Get(symbolStr)
	if !ok {
		return 0
	}

	bookTicker := symbol.GetBookTicker()
	if bookTicker == nil {
		return 0
	}

	// Use Bid price for LONG exit (what we'd get when selling)
	bidPrice, err := strconv.ParseFloat(bookTicker.BidPrice, 64)
	if err != nil {
		return 0
	}

	return bidPrice
}

// executeBuyOrder executes a buy order
func (gtb *GridTradingBot) executeBuyOrder(symbolStr string, quantity float64) error {
	pair, err := parsePairFromSymbol(symbolStr)
	if err != nil {
		return err
	}

	// Get symbol info for step size
	symbol, ok := gtb.OrderBook.Symbols.Get(symbolStr)
	if !ok {
		return fmt.Errorf("symbol %s not found in orderbook", symbolStr)
	}

	// Apply step size
	quantityDecimal := udecimal.MustFromFloat64(quantity)
	stepSizeQuantity, err := gtb.stepSizeQuantity(symbol, quantityDecimal)
	if err != nil {
		return err
	}

	qty, err := strconv.ParseFloat(stepSizeQuantity.StringFixed(uint8(symbol.BasePrecision)), 64)
	if err != nil {
		return err
	}

	// Execute market buy order
	res, err := gtb.NewMarketOrder(&pair, "BUY", qty)
	logger.Info(binance.PrettyPrint(res))

	return err
}

// executeSellOrder executes a sell order
func (gtb *GridTradingBot) executeSellOrder(symbolStr string, quantity float64) error {
	pair, err := parsePairFromSymbol(symbolStr)
	if err != nil {
		return err
	}

	// Get symbol info for step size
	symbol, ok := gtb.OrderBook.Symbols.Get(symbolStr)
	if !ok {
		return fmt.Errorf("symbol %s not found in orderbook", symbolStr)
	}

	// Apply step size
	quantityDecimal := udecimal.MustFromFloat64(quantity)
	stepSizeQuantity, err := gtb.stepSizeQuantity(symbol, quantityDecimal)
	if err != nil {
		return err
	}

	qty, err := strconv.ParseFloat(stepSizeQuantity.StringFixed(uint8(symbol.BasePrecision)), 64)
	if err != nil {
		return err
	}

	// Execute market sell order
	res, err := gtb.NewMarketOrder(&pair, "SELL", qty)
	logger.Info(binance.PrettyPrint(res))

	return err
}

// stepSizeQuantity applies step size to quantity (copied from arbitrage.go)
func (gtb *GridTradingBot) stepSizeQuantity(symbol *arbitrage.Symbol, quantity udecimal.Decimal) (udecimal.Decimal, error) {
	if symbol.Filter.LotSize.StepSize == "" {
		return quantity, nil
	}

	stepSize, err := udecimal.Parse(symbol.Filter.LotSize.StepSize)
	if err != nil {
		return udecimal.Decimal{}, err
	}

	lots, err := quantity.Div(stepSize)
	if err != nil {
		return udecimal.Decimal{}, err
	}

	lots = lots.Trunc(0)
	newQty := lots.Mul(stepSize)

	return newQty, nil
}

// NewMarketOrder creates a market order (copied from arbitrage.go)
func (gtb *GridTradingBot) NewMarketOrder(pair *currency.Pair, side string, quantity float64) (*binance.CreateOrderResponseFULL, error) {
	symbol := pair.String()

	order, err := gtb.Client.NewCreateOrderService().Symbol(symbol).
		Quantity(quantity).Type("MARKET").Side(side).Do(context.Background())
	if err != nil {
		return nil, err
	}

	res := order.(*binance.CreateOrderResponseFULL)
	return res, nil
}

// logStatus logs the current trading status
func (gtb *GridTradingBot) logStatus() {
	openPositions := 0
	for _, pos := range gtb.Positions {
		if pos.Status == "OPEN" {
			openPositions++
		}
	}

	if openPositions > 0 {
		logger.InfoFmt("Grid Trading Status: %d open positions", openPositions)

		// Log individual positions
		for symbol, pos := range gtb.Positions {
			if pos.Status == "OPEN" {
				currentPrice := gtb.getCurrentPrice(symbol)
				if currentPrice > 0 {
					profitLoss := (currentPrice - pos.EntryPrice) / pos.EntryPrice
					logger.DebugFmt("  %s: Entry=%.6f, Current=%.6f, P/L=%.2f%%",
						symbol, pos.EntryPrice, currentPrice, profitLoss*100)
				}
			}
		}
	} else {
		logger.DebugFmt("Grid Trading Status: No open positions")
	}
}

// GetStats returns current trading statistics
func (gtb *GridTradingBot) GetStats() map[string]any {
	gtb.mu.RLock()
	defer gtb.mu.RUnlock()

	openPositions := 0
	totalProfitLoss := 0.0

	for _, pos := range gtb.Positions {
		if pos.Status == "OPEN" {
			openPositions++
			currentPrice := gtb.getCurrentPrice(pos.Symbol)
			if currentPrice > 0 {
				profitLoss := (currentPrice - pos.EntryPrice) / pos.EntryPrice
				totalProfitLoss += profitLoss * gtb.Config.TriangleConfig.OrderUSDCAmount
			}
		}
	}

	return map[string]interface{}{
		"open_positions":    openPositions,
		"total_pnl_usdc":    totalProfitLoss,
		"monitored_symbols": len(gtb.ActiveSymbols),
	}
}
