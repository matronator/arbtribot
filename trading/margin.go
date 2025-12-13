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

// MarginPosition represents an open margin trade with trailing exit tracking.
type MarginPosition struct {
	Symbol      currency.Pair
	Side        string // LONG or SHORT
	EntryPrice  udecimal.Decimal
	Quantity    udecimal.Decimal
	EntryTime   time.Time
	Status      string
	PeakPrice   udecimal.Decimal // Highest seen price since entry (for LONG)
	TroughPrice udecimal.Decimal // Lowest seen price since entry (for SHORT)
	Notional    udecimal.Decimal
	BorrowedQty udecimal.Decimal // Amount borrowed for SHORT positions
}

type MarginBot struct {
	ob             *arbitrage.Orderbook
	cfg            *utils.Config
	tradeLogger    *zerolog.Logger
	simLogger      *zerolog.Logger
	client         *binance.Client
	trackedSymbols []currency.Pair
	positions      map[string]*MarginPosition
	history        map[string][]PricePoint
	simStats       *SimulationStats
	mu             sync.RWMutex
}

func NewMarginBot(ob *arbitrage.Orderbook, cfg *utils.Config, client *binance.Client, tradeLogger *zerolog.Logger) *MarginBot {
	tracked := make([]currency.Pair, 0, len(cfg.MarginConfig.QuoteAssets))
	history := make(map[string][]PricePoint)
	positions := make(map[string]*MarginPosition)

	for _, coin := range cfg.MarginConfig.QuoteAssets {
		if coin == "" {
			continue
		}
		pair := currency.Pair{
			Base:  currency.Currency{Symbol: coin},
			Quote: currency.Currency{Symbol: cfg.MarginConfig.BaseAsset},
		}
		tracked = append(tracked, pair)
		history[pair.String()] = make([]PricePoint, 0)
	}

	// Create simulation logger if in simulation mode
	var simLogger *zerolog.Logger
	if cfg.GeneralConfig.SimulationMode {
		sl := logger.NewSimTradeWriter()
		simLogger = &sl
		simLogger.Info().
			Str("mode", "MARGIN_SIMULATION").
			Strs("tracked_symbols", func() []string {
				symbols := make([]string, len(tracked))
				for i, p := range tracked {
					symbols[i] = p.String()
				}
				return symbols
			}()).
			Int("max_positions", cfg.MarginConfig.MaxPositions).
			Float64("entry_threshold_percent", cfg.MarginConfig.EntryChange*100).
			Float64("stop_loss_percent", cfg.MarginConfig.StopLoss*100).
			Float64("trailing_start_percent", cfg.MarginConfig.TrailingStart*100).
			Float64("trailing_gap_percent", cfg.MarginConfig.TrailingGap*100).
			Str("margin_type", cfg.MarginConfig.MarginType).
			Float64("position_size_usdt", cfg.MarginConfig.USDTPositionSize).
			Msg("Margin simulation started")
	}

	simStats := &SimulationStats{
		StartTime:       time.Now(),
		TotalPnL:        udecimal.Zero,
		TotalPnLPercent: udecimal.Zero,
		BestTradePnL:    udecimal.MustFromFloat64(-999999),
		WorstTradePnL:   udecimal.MustFromFloat64(999999),
		TotalVolume:     udecimal.Zero,
	}

	logger.InfoFmt("Margin bot initialized: tracking %d symbols, max positions=%d, entry threshold=%.2f%%, stop loss=%.2f%%, margin type=%s",
		len(tracked), cfg.MarginConfig.MaxPositions, cfg.MarginConfig.EntryChange*100, cfg.MarginConfig.StopLoss*100, cfg.MarginConfig.MarginType)
	for _, pair := range tracked {
		logger.InfoFmt("  - Tracking: %s", pair.String())
	}

	return &MarginBot{
		ob:             ob,
		cfg:            cfg,
		tradeLogger:    tradeLogger,
		simLogger:      simLogger,
		client:         client,
		trackedSymbols: tracked,
		positions:      positions,
		history:        history,
		simStats:       simStats,
	}
}

func (mb *MarginBot) Start(ctx context.Context) {
	go mb.loop(ctx)
}

func (mb *MarginBot) loop(ctx context.Context) {
	interval := time.Duration(mb.cfg.MarginConfig.CheckInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.InfoFmt("Margin bot loop started: check interval=%v, tracking %d symbols", interval, len(mb.trackedSymbols))

	cycleCount := 0
	lastStatusLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.InfoFmt("Margin bot loop stopped after %d cycles", cycleCount)
			return
		case <-ticker.C:
			cycleCount++
			mb.processCycle()

			// Log periodic status every 12 cycles (1 minute if 5s interval)
			if time.Since(lastStatusLog) > 60*time.Second {
				mb.mu.RLock()
				openPositions := len(mb.positions)
				totalHistoryPoints := 0
				symbolHistoryDetails := make(map[string]int)
				for symbolStr, h := range mb.history {
					count := len(h)
					totalHistoryPoints += count
					symbolHistoryDetails[symbolStr] = count
				}
				avgHistoryPoints := 0
				if len(mb.history) > 0 {
					avgHistoryPoints = totalHistoryPoints / len(mb.history)
				}
				mb.mu.RUnlock()

				logger.InfoFmt("Margin bot status: cycle=%d, open_positions=%d/%d, avg_history_points=%d/%d",
					cycleCount, openPositions, mb.cfg.MarginConfig.MaxPositions, avgHistoryPoints, mb.cfg.MarginConfig.LookbackPoints)

				// Log history details for each symbol
				for symbolStr, count := range symbolHistoryDetails {
					logger.InfoFmt("  %s: %d history points (need %d)", symbolStr, count, mb.cfg.MarginConfig.LookbackPoints)
				}

				lastStatusLog = time.Now()
			}
		}
	}
}

func (mb *MarginBot) processCycle() {
	mb.mu.Lock()
	defer mb.mu.Unlock()

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Margin bot cycle starting: %d positions open, %d symbols tracked", len(mb.positions), len(mb.trackedSymbols))
	}

	updated := mb.updatePriceHistory()
	mb.evaluateExistingPositions()
	scanned := mb.scanForEntries()

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Margin bot cycle complete: updated %d prices, scanned %d symbols, %d positions open", updated, scanned, len(mb.positions))
	}
}

func (mb *MarginBot) updatePriceHistory() int {
	updated := 0
	missing := 0
	noData := 0

	for _, symbol := range mb.trackedSymbols {
		symbolStr := symbol.String()
		symbolData, ok := mb.ob.Symbols.Get(symbolStr)
		if !ok {
			// Log warning if symbol is tracked but not in orderbook
			// This helps identify configuration issues
			if missing == 0 || mb.cfg.GeneralConfig.VerboseLogging {
				logger.WarningFmt("Symbol %s is tracked but not found in orderbook. Check if symbol exists in currency.AllSymbols", symbolStr)
			}
			missing++
			continue
		}

		book := symbolData.GetBookTicker()
		if book == nil {
			// Log warning if we have symbol but no price data
			// This indicates websocket might not be receiving updates
			historyCount := len(mb.history[symbolStr])
			if noData == 0 || mb.cfg.GeneralConfig.VerboseLogging {
				logger.WarningFmt("No book ticker data for %s (history: %d points). Websocket may not be receiving updates.", symbolStr, historyCount)
			}
			noData++
			continue
		}

		// Add price point if price has changed OR if we don't have enough history yet
		// This ensures we build up history even when prices are stable
		history := mb.history[symbolStr]
		shouldAdd := true
		if len(history) > 0 {
			lastPoint := history[len(history)-1]
			// Only skip if price unchanged AND we already have enough history points
			if lastPoint.BidPrice == book.BidPrice && lastPoint.AskPrice == book.AskPrice {
				// Still add if we don't have enough history yet (to build up initial history)
				if len(history) >= mb.cfg.MarginConfig.LookbackPoints {
					shouldAdd = false
				} else {
					// Add even if price unchanged, but only if last update was more than 1 second ago
					// This prevents too many duplicate entries while still building history
					if time.Since(lastPoint.Timestamp) < time.Second {
						shouldAdd = false
					}
				}
			}
		}

		if shouldAdd {
			mb.history[symbolStr] = append(mb.history[symbolStr], PricePoint{
				BidPrice:  book.BidPrice,
				AskPrice:  book.AskPrice,
				Timestamp: time.Now(),
			})

			if len(mb.history[symbolStr]) > 120 {
				mb.history[symbolStr] = mb.history[symbolStr][1:]
			}
		}

		updated++
		if mb.cfg.GeneralConfig.TraceLogging {
			logger.DebugFmt("Updated price history for %s: bid=%s ask=%s (history size: %d)", symbolStr, book.BidPrice, book.AskPrice, len(mb.history[symbolStr]))
		}
	}

	if mb.cfg.GeneralConfig.VerboseLogging && (missing > 0 || noData > 0) {
		logger.DebugFmt("Price history update: %d updated, %d missing from orderbook, %d no data", updated, missing, noData)
	}

	return updated
}

func (mb *MarginBot) evaluateExistingPositions() {
	if len(mb.positions) == 0 {
		if mb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("No open positions to evaluate")
		}
		return
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Evaluating %d open positions", len(mb.positions))
	}

	for symbolStr, position := range mb.positions {
		if position.Status != "OPEN" {
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping position %s: status=%s", symbolStr, position.Status)
			}
			continue
		}

		symbolData, ok := mb.ob.Symbols.Get(symbolStr)
		if !ok {
			logger.WarningFmt("Margin position %s missing from orderbook", symbolStr)
			continue
		}

		book := symbolData.GetBookTicker()
		if book == nil {
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("No book ticker for position %s", symbolStr)
			}
			continue
		}

		currentPrice, err := midPrice(book)
		if err != nil {
			logger.ErrorFmt("Failed to parse price for %s: %v", symbolStr, err)
			continue
		}

		priceChange, err := priceChangeFromEntryMargin(position, currentPrice)
		if err != nil {
			logger.ErrorFmt("Failed to calculate P&L for %s: %v", symbolStr, err)
			continue
		}
		// No leverage multiplier for margin trading (1:1 exposure)
		effectivePnL := priceChange

		// Update peak/trough for trailing logic
		if position.Side == "LONG" && currentPrice.Cmp(position.PeakPrice) > 0 {
			position.PeakPrice = currentPrice
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Updated peak price for %s LONG: %s", symbolStr, currentPrice.String())
			}
		}
		if position.Side == "SHORT" && currentPrice.Cmp(position.TroughPrice) < 0 {
			position.TroughPrice = currentPrice
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Updated trough price for %s SHORT: %s", symbolStr, currentPrice.String())
			}
		}

		holdDuration := time.Since(position.EntryTime)
		pnlPercent := effectivePnL.Mul(udecimal.MustFromFloat64(100))

		if mb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Position %s %s: entry=%s current=%s P&L=%.4f%% hold=%v",
				symbolStr, position.Side, position.EntryPrice.String(), currentPrice.String(),
				pnlPercent.InexactFloat64(), holdDuration.Round(time.Second))
		}

		// Stop loss
		stopLossThreshold := udecimal.MustFromFloat64(-mb.cfg.MarginConfig.StopLoss)
		if effectivePnL.Cmp(stopLossThreshold) <= 0 {
			logger.InfoFmt("Stop loss triggered for %s: P&L=%.4f%% <= %.4f%%", symbolStr, pnlPercent.InexactFloat64(), stopLossThreshold.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
			mb.closePosition(symbolStr, currentPrice, "STOP_LOSS")
			continue
		}

		// Time-based exit
		maxHoldTime := time.Duration(mb.cfg.NormalConfig.MaxHoldTime) * time.Minute
		if holdDuration > maxHoldTime {
			logger.InfoFmt("Max hold time reached for %s: %v > %v", symbolStr, holdDuration, maxHoldTime)
			mb.closePosition(symbolStr, currentPrice, "TIMEOUT")
			continue
		}

		// Trailing stop once target reached
		profitTarget := udecimal.MustFromFloat64(mb.cfg.MarginConfig.TrailingStart)
		if effectivePnL.Cmp(profitTarget) >= 0 {
			if mb.shouldTriggerTrail(position, currentPrice) {
				logger.InfoFmt("Trailing exit triggered for %s: P&L=%.4f%% >= %.4f%%", symbolStr, pnlPercent.InexactFloat64(), profitTarget.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
				mb.closePosition(symbolStr, currentPrice, "TRAILING_EXIT")
				continue
			} else if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Position %s has profit target but trailing not triggered yet", symbolStr)
			}
		}
	}
}

func (mb *MarginBot) shouldTriggerTrail(pos *MarginPosition, current udecimal.Decimal) bool {
	gapAllowed := udecimal.MustFromFloat64(mb.cfg.MarginConfig.TrailingGap)
	if pos.Side == "LONG" {
		drawdown, err := pos.PeakPrice.Sub(current).Div(pos.PeakPrice)
		if err != nil {
			return false
		}
		return drawdown.Cmp(gapAllowed) >= 0
	}

	// SHORT
	bounce, err := current.Sub(pos.TroughPrice).Div(pos.TroughPrice)
	if err != nil {
		return false
	}
	return bounce.Cmp(gapAllowed) >= 0
}

func (mb *MarginBot) scanForEntries() int {
	scanned := 0
	skippedMaxPositions := 0
	skippedAlreadyOpen := 0
	skippedInsufficientHistory := 0
	analyzed := 0

	for _, symbol := range mb.trackedSymbols {
		symbolStr := symbol.String()
		scanned++

		if len(mb.positions) >= mb.cfg.MarginConfig.MaxPositions {
			if mb.cfg.GeneralConfig.VerboseLogging && skippedMaxPositions == 0 {
				logger.DebugFmt("Max positions reached (%d/%d), skipping entry scan", len(mb.positions), mb.cfg.MarginConfig.MaxPositions)
			}
			skippedMaxPositions++
			continue
		}
		if _, exists := mb.positions[symbolStr]; exists {
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping %s: already has open position", symbolStr)
			}
			skippedAlreadyOpen++
			continue
		}

		history := mb.history[symbolStr]
		if len(history) < mb.cfg.MarginConfig.LookbackPoints {
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping %s: insufficient history (%d/%d points)", symbolStr, len(history), mb.cfg.MarginConfig.LookbackPoints)
			}
			skippedInsufficientHistory++
			continue
		}

		change, err := mb.analyzeMomentum(history)
		if err != nil {
			logger.ErrorFmt("Failed to analyze momentum for %s: %v", symbolStr, err)
			continue
		}

		analyzed++
		changePercent := change.Mul(udecimal.MustFromFloat64(100))
		threshold := udecimal.MustFromFloat64(mb.cfg.MarginConfig.EntryChange)
		thresholdPercent := threshold.Mul(udecimal.MustFromFloat64(100))
		changeFloat := changePercent.InexactFloat64()
		thresholdFloat := thresholdPercent.InexactFloat64()

		// Always log momentum analysis for debugging
		logger.InfoFmt("Momentum analysis for %s: change=%.4f%%, threshold=±%.4f%%, history_points=%d",
			symbolStr, changeFloat, thresholdFloat, len(history))

		if change.Cmp(threshold) >= 0 {
			logger.InfoFmt("LONG signal detected for %s: momentum=%.4f%% >= threshold=%.4f%%", symbolStr, changeFloat, thresholdFloat)
			mb.openPosition(symbol, "LONG")
		} else if change.Cmp(threshold.Neg()) <= 0 {
			logger.InfoFmt("SHORT signal detected for %s: momentum=%.4f%% <= threshold=%.4f%%", symbolStr, changeFloat, -thresholdFloat)
			mb.openPosition(symbol, "SHORT")
		} else {
			logger.DebugFmt("No entry signal for %s: momentum=%.4f%% (threshold: ±%.4f%%)", symbolStr, changeFloat, thresholdFloat)
		}
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Entry scan complete: scanned=%d, analyzed=%d, skipped: max_positions=%d, already_open=%d, insufficient_history=%d",
			scanned, analyzed, skippedMaxPositions, skippedAlreadyOpen, skippedInsufficientHistory)
	}

	return scanned
}

func (mb *MarginBot) analyzeMomentum(history []PricePoint) (udecimal.Decimal, error) {
	if len(history) < 2 {
		return udecimal.MustFromFloat64(0), fmt.Errorf("insufficient history")
	}

	// Use the configured lookback points, but ensure we have enough data
	lookback := mb.cfg.MarginConfig.LookbackPoints
	if lookback > len(history) {
		lookback = len(history)
	}
	if lookback < 2 {
		lookback = 2
	}

	startIdx := len(history) - lookback

	first, err := midPriceFromPoint(history[startIdx])
	if err != nil {
		return udecimal.MustFromFloat64(0), fmt.Errorf("failed to parse first price: %w", err)
	}
	last, err := midPriceFromPoint(history[len(history)-1])
	if err != nil {
		return udecimal.MustFromFloat64(0), fmt.Errorf("failed to parse last price: %w", err)
	}

	if first.IsZero() {
		return udecimal.MustFromFloat64(0), fmt.Errorf("zero reference price")
	}

	change, err := last.Sub(first).Div(first)
	if err != nil {
		return udecimal.MustFromFloat64(0), fmt.Errorf("failed to calculate change: %w", err)
	}

	// Log detailed momentum analysis if verbose
	if mb.cfg.GeneralConfig.VerboseLogging {
		firstPrice := first.InexactFloat64()
		lastPrice := last.InexactFloat64()
		changePercent := change.Mul(udecimal.MustFromFloat64(100)).InexactFloat64()
		logger.DebugFmt("Momentum analysis: first=%.8f, last=%.8f, change=%.4f%%, lookback=%d points",
			firstPrice, lastPrice, changePercent, lookback)
	}

	return change, nil
}

func (mb *MarginBot) openPosition(symbol currency.Pair, side string) {
	symbolData, ok := mb.ob.Symbols.Get(symbol.String())
	if !ok {
		logger.WarningFmt("Symbol %s not found for margin entry", symbol.String())
		return
	}

	book := symbolData.GetBookTicker()
	if book == nil {
		return
	}

	entryPrice, err := midPrice(book)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for %s: %v", symbol.String(), err)
		return
	}

	rawQty, err := udecimal.MustFromFloat64(mb.cfg.MarginConfig.USDTPositionSize).Div(entryPrice)
	if err != nil {
		logger.ErrorFmt("Failed to derive raw quantity for %s: %v", symbol.String(), err)
		return
	}
	validatedQty, err := arbitrage.StepSizeQuantity(symbolData, rawQty)
	if err != nil {
		logger.ErrorFmt("Failed to apply step size for %s: %v", symbol.String(), err)
		return
	}

	position := &MarginPosition{
		Symbol:      symbol,
		Side:        side,
		EntryPrice:  entryPrice,
		Quantity:    validatedQty,
		EntryTime:   time.Now(),
		Status:      "OPEN",
		PeakPrice:   entryPrice,
		TroughPrice: entryPrice,
		Notional:    entryPrice.Mul(validatedQty),
		BorrowedQty: udecimal.Zero,
	}

	if !mb.cfg.GeneralConfig.SimulationMode {
		// Use regular spot orders for LONG positions (buy with own funds)
		// Use margin orders only for SHORT positions (borrow to sell)
		switch side {
		case "LONG":
			if err := mb.placeSpotOrder(symbol, "BUY", validatedQty); err != nil {
				logger.ErrorFmt("Live spot order failed for %s: %v", symbol.String(), err)
				return
			}
		case "SHORT":
			if err := mb.placeMarginOrder(symbol, "SHORT", validatedQty); err != nil {
				logger.ErrorFmt("Live margin order failed for %s: %v", symbol.String(), err)
				return
			}
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would open %s %s qty %s at %s", side, symbol.String(), validatedQty.String(), entryPrice.String())))
	}

	mb.positions[symbol.String()] = position

	// Log simulation trade
	if mb.cfg.GeneralConfig.SimulationMode && mb.simLogger != nil {
		mb.logSimulationTrade("OPEN", symbol.String(), position, entryPrice, udecimal.Zero, udecimal.Zero, "ENTRY", time.Duration(0))
	}

	logger.InfoFmt("Opened margin %s for %s: entry=%s qty=%s", side, symbol.String(), entryPrice.String(), validatedQty.String())
	mb.tradeLogger.Info().
		Str("action", "OPEN_MARGIN_POSITION").
		Str("symbol", symbol.String()).
		Str("side", side).
		Str("entry_price", entryPrice.String()).
		Str("quantity", validatedQty.String()).
		Str("margin_type", mb.cfg.MarginConfig.MarginType).
		Time("entry_time", position.EntryTime).
		Msg("Margin position opened")
}

func (mb *MarginBot) closePosition(symbolStr string, exitPrice udecimal.Decimal, reason string) {
	position, exists := mb.positions[symbolStr]
	if !exists || position.Status != "OPEN" {
		return
	}

	priceChange, err := priceChangeFromEntryMargin(position, exitPrice)
	if err != nil {
		logger.ErrorFmt("Failed to calculate exit P&L for %s: %v", symbolStr, err)
		return
	}
	// No leverage for margin trading
	effectivePnL := priceChange
	profitAmount := effectivePnL.Mul(position.Notional)
	holdDuration := time.Since(position.EntryTime)

	exitSide := "SELL"
	if position.Side == "SHORT" {
		exitSide = "BUY"
	}

	if !mb.cfg.GeneralConfig.SimulationMode {
		// Use regular spot orders for closing LONG positions
		// Use margin orders with AUTO_REPAY for closing SHORT positions
		switch position.Side {
		case "LONG":
			if err := mb.placeSpotOrder(position.Symbol, exitSide, position.Quantity); err != nil {
				logger.ErrorFmt("Failed to close LONG position %s: %v", symbolStr, err)
				return
			}
		case "SHORT":
			// Use margin order with AUTO_REPAY to buy back and automatically repay borrowed assets
			if err := mb.placeMarginOrderWithRepay(position.Symbol, exitSide, position.Quantity, true); err != nil {
				logger.ErrorFmt("Failed to close SHORT position %s: %v", symbolStr, err)
				return
			}
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would close %s via %s %s at %s", symbolStr, exitSide, position.Quantity.String(), exitPrice.String())))
	}

	position.Status = "CLOSED"
	delete(mb.positions, symbolStr)

	// Update simulation statistics
	if mb.cfg.GeneralConfig.SimulationMode && mb.simLogger != nil {
		mb.updateSimStats(profitAmount, effectivePnL, position.Notional, holdDuration)
		mb.logSimulationTrade("CLOSE", symbolStr, position, exitPrice, effectivePnL, profitAmount, reason, holdDuration)
	}

	logger.InfoFmt("Closed margin %s: P&L=%s%% amount=%s reason=%s", symbolStr, effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(3), profitAmount.StringFixed(4), logger.ColorizeReason(reason))
	mb.tradeLogger.Info().
		Str("action", "CLOSE_MARGIN_POSITION").
		Str("symbol", symbolStr).
		Str("side", position.Side).
		Str("entry_price", position.EntryPrice.String()).
		Str("exit_price", exitPrice.String()).
		Str("pnl_percent", effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(3)).
		Str("pnl_amount", profitAmount.StringFixed(4)).
		Str("reason", reason).
		Time("exit_time", time.Now()).
		Msg("Margin position closed")
}

func (mb *MarginBot) placeMarginOrderWithRepay(pair currency.Pair, side string, qty udecimal.Decimal, autoRepay bool) error {
	if mb.client == nil {
		return fmt.Errorf("margin client not configured")
	}
	symbolData, hasSymbol := mb.ob.Symbols.Get(pair.String())
	var formattedQty string
	if hasSymbol {
		formattedQty = qty.StringFixed(uint8(symbolData.BasePrecision))
	} else {
		formattedQty = qty.String()
	}
	// Ensure formattedQty can be parsed for validation
	if _, err := strconv.ParseFloat(formattedQty, 64); err != nil {
		return fmt.Errorf("failed to format quantity: %w", err)
	}

	formattedQtyFloat, err := strconv.ParseFloat(formattedQty, 64)
	if err != nil {
		return fmt.Errorf("failed to parse quantity: %w", err)
	}

	// Place margin order using Binance Margin API
	// SideEffectType: "MARGIN_BUY" for auto-borrow, "AUTO_REPAY" for auto-repay
	sideEffect := "MARGIN_BUY"
	if autoRepay {
		sideEffect = "AUTO_REPAY" // Auto-repay when closing positions
	}

	orderService := mb.client.NewMarginAccountNewOrderService().
		Symbol(pair.String()).
		Quantity(formattedQtyFloat).
		OrderType("MARKET").
		Side(side).
		SideEffectType(sideEffect)

	// Set margin type (ISOLATED or CROSS)
	if mb.cfg.MarginConfig.MarginType == "ISOLATED" {
		orderService = orderService.IsIsolated("TRUE")
	} else {
		orderService = orderService.IsIsolated("FALSE")
	}

	order, err := orderService.Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to place margin order: %w", err)
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.InfoFmt("Margin order response: %+v", order)
	}
	logger.InfoFmt("Margin order placed for %s %s %s at market (type: %s, sideEffect: %s)", pair.String(), side, formattedQty, mb.cfg.MarginConfig.MarginType, sideEffect)
	return nil
}

// placeSpotOrder places a regular spot order (for LONG positions)
func (mb *MarginBot) placeSpotOrder(pair currency.Pair, side string, qty udecimal.Decimal) error {
	if mb.client == nil {
		return fmt.Errorf("spot client not configured")
	}
	symbolData, hasSymbol := mb.ob.Symbols.Get(pair.String())
	var formattedQty string
	if hasSymbol {
		formattedQty = qty.StringFixed(uint8(symbolData.BasePrecision))
	} else {
		formattedQty = qty.String()
	}
	// Ensure formattedQty can be parsed for validation
	if _, err := strconv.ParseFloat(formattedQty, 64); err != nil {
		return fmt.Errorf("failed to format quantity: %w", err)
	}

	formattedQtyFloat, err := strconv.ParseFloat(formattedQty, 64)
	if err != nil {
		return fmt.Errorf("failed to parse quantity: %w", err)
	}

	// Place regular spot order (no margin, no borrowing)
	order, err := mb.client.NewCreateOrderService().Symbol(pair.String()).
		Quantity(formattedQtyFloat).Type("MARKET").Side(side).Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to place spot order: %w", err)
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.InfoFmt("Spot order response: %+v", order)
	}
	logger.InfoFmt("Spot order placed for %s %s %s at market", pair.String(), side, formattedQty)
	return nil
}

// placeMarginOrder places a margin order (only for SHORT positions)
func (mb *MarginBot) placeMarginOrder(pair currency.Pair, side string, qty udecimal.Decimal) error {
	if mb.client == nil {
		return fmt.Errorf("margin client not configured")
	}
	symbolData, hasSymbol := mb.ob.Symbols.Get(pair.String())
	var formattedQty string
	if hasSymbol {
		formattedQty = qty.StringFixed(uint8(symbolData.BasePrecision))
	} else {
		formattedQty = qty.String()
	}
	// Ensure formattedQty can be parsed for validation
	if _, err := strconv.ParseFloat(formattedQty, 64); err != nil {
		return fmt.Errorf("failed to format quantity: %w", err)
	}

	formattedQtyFloat, err := strconv.ParseFloat(formattedQty, 64)
	if err != nil {
		return fmt.Errorf("failed to parse quantity: %w", err)
	}

	// SHORT positions: convert to SELL and use margin to borrow base asset
	if side == "SHORT" {
		side = "SELL"
		// Track that we'll need to repay later
		if pos, exists := mb.positions[pair.String()]; exists {
			pos.BorrowedQty = qty
		}
	} else {
		return fmt.Errorf("placeMarginOrder should only be called for SHORT positions, got: %s", side)
	}

	// Place margin order using Binance Margin API
	// SideEffectType: "MARGIN_BUY" for auto-borrow when opening SHORT positions
	sideEffect := "MARGIN_BUY"

	orderService := mb.client.NewMarginAccountNewOrderService().
		Symbol(pair.String()).
		Quantity(formattedQtyFloat).
		OrderType("MARKET").
		Side(side).
		SideEffectType(sideEffect) // Auto-borrow for margin orders

	// Set margin type (ISOLATED or CROSS)
	if mb.cfg.MarginConfig.MarginType == "ISOLATED" {
		orderService = orderService.IsIsolated("TRUE")
	} else {
		orderService = orderService.IsIsolated("FALSE")
	}

	order, err := orderService.Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to place margin order: %w", err)
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.InfoFmt("Margin order response: %+v", order)
	}
	logger.InfoFmt("Margin order placed for %s %s %s at market (type: %s)", pair.String(), side, formattedQty, mb.cfg.MarginConfig.MarginType)
	return nil
}

func (mb *MarginBot) borrowAsset(asset string, qty udecimal.Decimal) error {
	if mb.cfg.GeneralConfig.SimulationMode {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would borrow %s %s", qty.String(), asset)))
		return nil
	}

	formattedQty := qty.String()
	_, err := strconv.ParseFloat(formattedQty, 64)
	if err != nil {
		return fmt.Errorf("failed to parse borrow quantity: %w", err)
	}

	// Borrow asset using Binance Margin API
	// Note: This requires direct API call as binance-connector-go may not have built-in margin methods
	// For now, we log the action - in production, this needs to be implemented via direct HTTP calls
	// TODO: Implement proper margin borrow API call when library support is available
	// For simulation mode, this will work fine
	// In production, you would make a POST request to /sapi/v1/margin/loan with:
	// asset, amount, isIsolated parameters
	if !mb.cfg.GeneralConfig.SimulationMode {
		logger.WarningFmt("Margin borrow API call not fully implemented - please implement direct HTTP call to /sapi/v1/margin/loan")
		return fmt.Errorf("margin borrow not implemented for live trading - requires direct API call")
	}

	logger.InfoFmt("Borrowed %s %s for margin trading", formattedQty, asset)
	return nil
}

func (mb *MarginBot) repayLoan(asset string, qty udecimal.Decimal) error {
	if mb.cfg.GeneralConfig.SimulationMode {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would repay %s %s", qty.String(), asset)))
		return nil
	}

	formattedQty := qty.String()
	_, err := strconv.ParseFloat(formattedQty, 64)
	if err != nil {
		return fmt.Errorf("failed to parse repay quantity: %w", err)
	}

	// Repay loan using Binance Margin API
	// Note: This requires direct API call as binance-connector-go may not have built-in margin methods
	// For now, we log the action - in production, this needs to be implemented via direct HTTP calls
	// TODO: Implement proper margin repay API call when library support is available
	// For simulation mode, this will work fine
	// In production, you would make a POST request to /sapi/v1/margin/repay with:
	// asset, amount, isIsolated parameters
	if !mb.cfg.GeneralConfig.SimulationMode {
		logger.WarningFmt("Margin repay API call not fully implemented - please implement direct HTTP call to /sapi/v1/margin/repay")
		return fmt.Errorf("margin repay not implemented for live trading - requires direct API call")
	}

	logger.InfoFmt("Repaid %s %s loan", formattedQty, asset)
	return nil
}

func priceChangeFromEntryMargin(pos *MarginPosition, current udecimal.Decimal) (udecimal.Decimal, error) {
	if pos.Side == "LONG" {
		return current.Sub(pos.EntryPrice).Div(pos.EntryPrice)
	}
	return pos.EntryPrice.Sub(current).Div(pos.EntryPrice)
}

func (mb *MarginBot) logSimulationTrade(action, symbolStr string, position *MarginPosition, price udecimal.Decimal, pnlPercent, pnlAmount udecimal.Decimal, reason string, holdDuration time.Duration) {
	if mb.simLogger == nil {
		return
	}

	entry := mb.simLogger.Info().
		Str("action", action).
		Str("symbol", symbolStr).
		Str("side", position.Side).
		Str("entry_price", position.EntryPrice.String()).
		Str("quantity", position.Quantity.String()).
		Str("margin_type", mb.cfg.MarginConfig.MarginType).
		Str("notional_usdt", position.Notional.StringFixed(2)).
		Time("entry_time", position.EntryTime)

	if action == "CLOSE" {
		entry = entry.
			Str("exit_price", price.String()).
			Str("pnl_percent", pnlPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)).
			Str("pnl_amount_usdt", pnlAmount.StringFixed(4)).
			Str("reason", reason).
			Str("hold_duration", holdDuration.Round(time.Second).String()).
			Time("exit_time", time.Now())
	}

	entry.Msg("MARGIN_SIMULATION_TRADE")
}

func (mb *MarginBot) updateSimStats(pnlAmount, pnlPercent, volume udecimal.Decimal, holdDuration time.Duration) {
	mb.simStats.mu.Lock()
	defer mb.simStats.mu.Unlock()

	mb.simStats.TotalTrades++
	mb.simStats.TotalPnL = mb.simStats.TotalPnL.Add(pnlAmount)
	mb.simStats.TotalVolume = mb.simStats.TotalVolume.Add(volume)
	mb.simStats.LastTradeTime = time.Now()

	// Update best/worst trade
	if pnlAmount.Cmp(mb.simStats.BestTradePnL) > 0 {
		mb.simStats.BestTradePnL = pnlAmount
	}
	if pnlAmount.Cmp(mb.simStats.WorstTradePnL) < 0 {
		mb.simStats.WorstTradePnL = pnlAmount
	}

	// Count wins/losses
	if pnlAmount.Cmp(udecimal.Zero) > 0 {
		mb.simStats.WinningTrades++
	} else if pnlAmount.Cmp(udecimal.Zero) < 0 {
		mb.simStats.LosingTrades++
	}

	// Calculate average P&L percent
	if mb.simStats.TotalTrades > 0 && !mb.simStats.TotalVolume.IsZero() {
		if avgPnLPercent, err := mb.simStats.TotalPnL.Div(mb.simStats.TotalVolume); err == nil {
			mb.simStats.TotalPnLPercent = avgPnLPercent
		}
	}

	// Log periodic summary every 10 trades
	if mb.simStats.TotalTrades%10 == 0 {
		mb.logSimulationSummary()
	}
}

func (mb *MarginBot) logSimulationSummary() {
	if mb.simLogger == nil {
		return
	}

	mb.simStats.mu.RLock()
	defer mb.simStats.mu.RUnlock()

	winRate := udecimal.Zero
	if mb.simStats.TotalTrades > 0 {
		if wr, err := udecimal.MustFromFloat64(float64(mb.simStats.WinningTrades)).Div(udecimal.MustFromFloat64(float64(mb.simStats.TotalTrades))); err == nil {
			winRate = wr
		}
	}

	avgPnL := udecimal.Zero
	if mb.simStats.TotalTrades > 0 {
		if avg, err := mb.simStats.TotalPnL.Div(udecimal.MustFromFloat64(float64(mb.simStats.TotalTrades))); err == nil {
			avgPnL = avg
		}
	}

	runTime := time.Since(mb.simStats.StartTime)

	mb.simLogger.Info().
		Str("summary_type", "PERIODIC").
		Int("total_trades", mb.simStats.TotalTrades).
		Int("winning_trades", mb.simStats.WinningTrades).
		Int("losing_trades", mb.simStats.LosingTrades).
		Str("win_rate_percent", winRate.Mul(udecimal.MustFromFloat64(100)).StringFixed(2)).
		Str("total_pnl_usdt", mb.simStats.TotalPnL.StringFixed(4)).
		Str("total_pnl_percent", mb.simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)).
		Str("avg_pnl_per_trade_usdt", avgPnL.StringFixed(4)).
		Str("best_trade_usdt", mb.simStats.BestTradePnL.StringFixed(4)).
		Str("worst_trade_usdt", mb.simStats.WorstTradePnL.StringFixed(4)).
		Str("total_volume_usdt", mb.simStats.TotalVolume.StringFixed(2)).
		Str("runtime", runTime.Round(time.Second).String()).
		Time("last_trade_time", mb.simStats.LastTradeTime).
		Msg("MARGIN_SIMULATION_SUMMARY")
}

func (mb *MarginBot) LogFinalSimulationSummary() {
	if !mb.cfg.GeneralConfig.SimulationMode || mb.simLogger == nil {
		return
	}

	mb.simStats.mu.RLock()
	defer mb.simStats.mu.RUnlock()

	winRate := udecimal.Zero
	if mb.simStats.TotalTrades > 0 {
		if wr, err := udecimal.MustFromFloat64(float64(mb.simStats.WinningTrades)).Div(udecimal.MustFromFloat64(float64(mb.simStats.TotalTrades))); err == nil {
			winRate = wr
		}
	}

	avgPnL := udecimal.Zero
	if mb.simStats.TotalTrades > 0 {
		if avg, err := mb.simStats.TotalPnL.Div(udecimal.MustFromFloat64(float64(mb.simStats.TotalTrades))); err == nil {
			avgPnL = avg
		}
	}

	runTime := time.Since(mb.simStats.StartTime)

	mb.simLogger.Info().
		Str("summary_type", "FINAL").
		Int("total_trades", mb.simStats.TotalTrades).
		Int("winning_trades", mb.simStats.WinningTrades).
		Int("losing_trades", mb.simStats.LosingTrades).
		Str("win_rate_percent", winRate.Mul(udecimal.MustFromFloat64(100)).StringFixed(2)).
		Str("total_pnl_usdt", mb.simStats.TotalPnL.StringFixed(4)).
		Str("total_pnl_percent", mb.simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)).
		Str("avg_pnl_per_trade_usdt", avgPnL.StringFixed(4)).
		Str("best_trade_usdt", mb.simStats.BestTradePnL.StringFixed(4)).
		Str("worst_trade_usdt", mb.simStats.WorstTradePnL.StringFixed(4)).
		Str("total_volume_usdt", mb.simStats.TotalVolume.StringFixed(2)).
		Str("runtime", runTime.Round(time.Second).String()).
		Time("start_time", mb.simStats.StartTime).
		Time("end_time", time.Now()).
		Msg("MARGIN_SIMULATION_FINAL_SUMMARY")

	logger.InfoFmt("=== MARGIN SIMULATION FINAL SUMMARY ===")
	logger.InfoFmt("Total Trades: %d (Wins: %d, Losses: %d, Win Rate: %.2f%%)",
		mb.simStats.TotalTrades, mb.simStats.WinningTrades, mb.simStats.LosingTrades, winRate.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())

	totalPnLPercent := udecimal.Zero
	if !mb.simStats.TotalVolume.IsZero() {
		if pnlPct, err := mb.simStats.TotalPnL.Div(mb.simStats.TotalVolume); err == nil {
			totalPnLPercent = pnlPct
		}
	}

	logger.InfoFmt("Total P&L: %s USDT (%.4f%%)", mb.simStats.TotalPnL.StringFixed(4), totalPnLPercent.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
	logger.InfoFmt("Average P&L per Trade: %s USDT", avgPnL.StringFixed(4))
	logger.InfoFmt("Best Trade: %s USDT | Worst Trade: %s USDT", mb.simStats.BestTradePnL.StringFixed(4), mb.simStats.WorstTradePnL.StringFixed(4))
	logger.InfoFmt("Total Volume: %s USDT", mb.simStats.TotalVolume.StringFixed(2))
	logger.InfoFmt("Runtime: %v", runTime.Round(time.Second))
	logger.InfoFmt("========================================")
}
