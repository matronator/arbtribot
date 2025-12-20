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

	futures "github.com/adshao/go-binance/v2/futures"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog"
)

// FuturesPosition represents an open futures trade with trailing exit tracking.
type FuturesPosition struct {
	Symbol      currency.Pair
	Side        string // LONG or SHORT
	EntryPrice  udecimal.Decimal
	Quantity    udecimal.Decimal
	EntryTime   time.Time
	Status      string
	PeakPrice   udecimal.Decimal // Highest seen price since entry (for LONG)
	TroughPrice udecimal.Decimal // Lowest seen price since entry (for SHORT)
	Notional    udecimal.Decimal
	Leverage    int
}

type SimulationStats struct {
	TotalTrades     int
	WinningTrades   int
	LosingTrades    int
	TotalPnL        udecimal.Decimal
	TotalPnLPercent udecimal.Decimal
	BestTradePnL    udecimal.Decimal
	WorstTradePnL   udecimal.Decimal
	TotalVolume     udecimal.Decimal
	StartTime       time.Time
	LastTradeTime   time.Time
	mu              sync.RWMutex
}

type ClosedFuturesPosition struct {
	EntryPosition FuturesPosition
	ClosingPrice  string
	ClosingTime   time.Time
	Profit        string
	PnLPercent    string
	Reason        string
	HoldDuration  string
}

type FuturesBot struct {
	ob              *arbitrage.Orderbook
	cfg             *utils.Config
	tradeLogger     *zerolog.Logger
	simLogger       *zerolog.Logger
	futuresClient   *futures.Client
	trackedSymbols  []currency.Pair
	positions       map[string]*FuturesPosition
	closedPositions map[string][]ClosedFuturesPosition
	history         map[string][]PricePoint
	simStats        *SimulationStats
	mu              sync.RWMutex
}

func NewFuturesBot(ob *arbitrage.Orderbook, cfg *utils.Config, futuresClient *futures.Client, tradeLogger *zerolog.Logger) *FuturesBot {
	tracked := make([]currency.Pair, 0, len(cfg.FuturesConfig.QuoteAssets))
	history := make(map[string][]PricePoint)
	positions := make(map[string]*FuturesPosition)

	for _, coin := range cfg.FuturesConfig.QuoteAssets {
		if coin == "" {
			continue
		}
		pair := currency.Pair{
			Base:  currency.Currency{Symbol: coin},
			Quote: currency.Currency{Symbol: cfg.FuturesConfig.BaseAsset},
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
			Str("mode", "FUTURES_SIMULATION").
			Strs("tracked_symbols", func() []string {
				symbols := make([]string, len(tracked))
				for i, p := range tracked {
					symbols[i] = p.String()
				}
				return symbols
			}()).
			Int("max_positions", cfg.FuturesConfig.MaxPositions).
			Float64("entry_threshold_percent", cfg.FuturesConfig.EntryChange*100).
			Float64("stop_loss_percent", cfg.FuturesConfig.StopLoss*100).
			Float64("trailing_start_percent", cfg.FuturesConfig.TrailingStart*100).
			Float64("trailing_gap_percent", cfg.FuturesConfig.TrailingGap*100).
			Int("leverage", cfg.FuturesConfig.Leverage).
			Float64("position_size_usdt", cfg.FuturesConfig.USDTPositionSize).
			Msg("Futures simulation started")
	}

	simStats := &SimulationStats{
		StartTime:       time.Now(),
		TotalPnL:        udecimal.Zero,
		TotalPnLPercent: udecimal.Zero,
		BestTradePnL:    udecimal.MustFromFloat64(-999999),
		WorstTradePnL:   udecimal.MustFromFloat64(999999),
		TotalVolume:     udecimal.Zero,
	}

	logger.InfoFmt("Futures bot initialized: tracking %d symbols, max positions=%d, entry threshold=%.2f%%, stop loss=%.2f%%, leverage=%dx",
		len(tracked), cfg.FuturesConfig.MaxPositions, cfg.FuturesConfig.EntryChange*100, cfg.FuturesConfig.StopLoss*100, cfg.FuturesConfig.Leverage)
	for _, pair := range tracked {
		logger.InfoFmt("  - Tracking: %s", pair.String())
	}

	return &FuturesBot{
		ob:              ob,
		cfg:             cfg,
		tradeLogger:     tradeLogger,
		simLogger:       simLogger,
		futuresClient:   futuresClient,
		trackedSymbols:  tracked,
		positions:       positions,
		closedPositions: make(map[string][]ClosedFuturesPosition),
		history:         history,
		simStats:        simStats,
	}
}

// Positions returns the positions map (for persistence)
func (fb *FuturesBot) Positions() map[string]*FuturesPosition {
	return fb.positions
}

// Mu returns the mutex (for thread-safe access)
func (fb *FuturesBot) Mu() *sync.RWMutex {
	return &fb.mu
}

func (fb *FuturesBot) Start(ctx context.Context) {
	go fb.loop(ctx)
}

func (fb *FuturesBot) loop(ctx context.Context) {
	interval := time.Duration(fb.cfg.FuturesConfig.CheckInterval) * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	logger.InfoFmt("Futures bot loop started: check interval=%v, tracking %d symbols", interval, len(fb.trackedSymbols))

	cycleCount := 0
	lastStatusLog := time.Now()

	for {
		select {
		case <-ctx.Done():
			logger.InfoFmt("Futures bot loop stopped after %d cycles", cycleCount)
			// Don't log final summary here - it will be logged in main.go shutdown handler
			return
		case <-ticker.C:
			cycleCount++
			fb.processCycle()

			// Log periodic status every 12 cycles (1 minute if 5s interval)
			if time.Since(lastStatusLog) > 60*time.Second {
				fb.mu.RLock()
				openPositions := len(fb.positions)
				totalHistoryPoints := 0
				symbolHistoryDetails := make(map[string]int)
				for symbolStr, h := range fb.history {
					count := len(h)
					totalHistoryPoints += count
					symbolHistoryDetails[symbolStr] = count
				}
				avgHistoryPoints := 0
				if len(fb.history) > 0 {
					avgHistoryPoints = totalHistoryPoints / len(fb.history)
				}
				fb.mu.RUnlock()

				logger.InfoFmt("Futures bot status: cycle=%d, open_positions=%d/%d, avg_history_points=%d/%d",
					cycleCount, openPositions, fb.cfg.FuturesConfig.MaxPositions, avgHistoryPoints, fb.cfg.FuturesConfig.LookbackPoints)

				// Log history details for each symbol
				for symbolStr, count := range symbolHistoryDetails {
					logger.InfoFmt("  %s: %d history points (need %d)", symbolStr, count, fb.cfg.FuturesConfig.LookbackPoints)
				}

				lastStatusLog = time.Now()
			}
		}
	}
}

func (fb *FuturesBot) processCycle() {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	if fb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Futures bot cycle starting: %d positions open, %d symbols tracked", len(fb.positions), len(fb.trackedSymbols))
	}

	updated := fb.updatePriceHistory()
	fb.evaluateExistingPositions()
	scanned := fb.scanForEntries()

	if fb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Futures bot cycle complete: updated %d prices, scanned %d symbols, %d positions open", updated, scanned, len(fb.positions))
	}
}

func (fb *FuturesBot) updatePriceHistory() int {
	updated := 0
	missing := 0
	noData := 0

	for _, symbol := range fb.trackedSymbols {
		symbolStr := symbol.String()
		symbolData, ok := fb.ob.Symbols.Get(symbolStr)
		if !ok {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Symbol %s not found in orderbook", symbolStr)
			}
			missing++
			continue
		}

		book := symbolData.GetBookTicker()
		if book == nil {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("No book ticker data for %s", symbolStr)
			}
			noData++
			continue
		}

		// Only add price point if price has changed (avoid duplicate entries)
		history := fb.history[symbolStr]
		shouldAdd := true
		if len(history) > 0 {
			lastPoint := history[len(history)-1]
			if lastPoint.BidPrice == book.BidPrice && lastPoint.AskPrice == book.AskPrice {
				shouldAdd = false
			}
		}

		if shouldAdd {
			fb.history[symbolStr] = append(fb.history[symbolStr], PricePoint{
				BidPrice:  book.BidPrice,
				AskPrice:  book.AskPrice,
				Timestamp: time.Now(),
			})

			if len(fb.history[symbolStr]) > 120 {
				fb.history[symbolStr] = fb.history[symbolStr][1:]
			}
		}

		updated++
		if fb.cfg.GeneralConfig.TraceLogging {
			logger.DebugFmt("Updated price history for %s: bid=%s ask=%s (history size: %d)", symbolStr, book.BidPrice, book.AskPrice, len(fb.history[symbolStr]))
		}
	}

	if fb.cfg.GeneralConfig.VerboseLogging && (missing > 0 || noData > 0) {
		logger.DebugFmt("Price history update: %d updated, %d missing from orderbook, %d no data", updated, missing, noData)
	}

	return updated
}

func (fb *FuturesBot) evaluateExistingPositions() {
	if len(fb.positions) == 0 {
		if fb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("No open positions to evaluate")
		}
		return
	}

	if fb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Evaluating %d open positions", len(fb.positions))
	}

	for symbolStr, position := range fb.positions {
		if position.Status != "OPEN" {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping position %s: status=%s", symbolStr, position.Status)
			}
			continue
		}

		symbolData, ok := fb.ob.Symbols.Get(symbolStr)
		if !ok {
			logger.WarningFmt("Futures position %s missing from orderbook", symbolStr)
			continue
		}

		book := symbolData.GetBookTicker()
		if book == nil {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("No book ticker for position %s", symbolStr)
			}
			continue
		}

		currentPrice, err := midPrice(book)
		if err != nil {
			logger.ErrorFmt("Failed to parse price for %s: %v", symbolStr, err)
			continue
		}

		priceChange, err := priceChangeFromEntry(position, currentPrice)
		if err != nil {
			logger.ErrorFmt("Failed to calculate P&L for %s: %v", symbolStr, err)
			continue
		}
		effectivePnL := priceChange.Mul(udecimal.MustFromFloat64(float64(position.Leverage)))

		// Update peak/trough for trailing logic
		if position.Side == "LONG" && currentPrice.Cmp(position.PeakPrice) > 0 {
			position.PeakPrice = currentPrice
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Updated peak price for %s LONG: %s", symbolStr, currentPrice.String())
			}
		}
		if position.Side == "SHORT" && currentPrice.Cmp(position.TroughPrice) < 0 {
			position.TroughPrice = currentPrice
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Updated trough price for %s SHORT: %s", symbolStr, currentPrice.String())
			}
		}

		holdDuration := time.Since(position.EntryTime)
		pnlPercent := effectivePnL.Mul(udecimal.MustFromFloat64(100))

		if fb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Position %s %s: entry=%s current=%s P&L=%.4f%% hold=%v",
				symbolStr, position.Side, position.EntryPrice.String(), currentPrice.String(),
				pnlPercent.InexactFloat64(), holdDuration.Round(time.Second))
		}

		// Stop loss
		stopLossThreshold := udecimal.MustFromFloat64(-fb.cfg.FuturesConfig.StopLoss)
		if effectivePnL.Cmp(stopLossThreshold) <= 0 {
			logger.InfoFmt("Stop loss triggered for %s: P&L=%.4f%% <= %.4f%%", symbolStr, pnlPercent.InexactFloat64(), stopLossThreshold.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
			fb.closePosition(symbolStr, currentPrice, "STOP_LOSS")
			continue
		}

		// Time-based exit
		// Only close on timeout if position is profitable (P&L > 0)
		// If losing money, keep it open unless stop loss is reached
		maxHoldTime := time.Duration(fb.cfg.NormalConfig.MaxHoldTime) * time.Minute
		if holdDuration > maxHoldTime {
			if effectivePnL.Cmp(udecimal.Zero) > 0 {
				logger.InfoFmt("Max hold time reached for %s with positive P&L (%.4f%%). Closing position.", symbolStr, pnlPercent.InexactFloat64())
				fb.closePosition(symbolStr, currentPrice, "TIMEOUT")
			} else {
				logger.InfoFmt("Max hold time reached for %s but position is unprofitable (P&L=%.4f%%). Keeping position open until stop loss.", symbolStr, pnlPercent.InexactFloat64())
			}
			continue
		}

		// Trailing stop once target reached
		profitTarget := udecimal.MustFromFloat64(fb.cfg.FuturesConfig.TrailingStart)
		if effectivePnL.Cmp(profitTarget) >= 0 {
			if fb.shouldTriggerTrail(position, currentPrice) {
				logger.InfoFmt("Trailing exit triggered for %s: P&L=%.4f%% >= %.4f%%", symbolStr, pnlPercent.InexactFloat64(), profitTarget.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
				fb.closePosition(symbolStr, currentPrice, "TRAILING_EXIT")
				continue
			} else if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Position %s has profit target but trailing not triggered yet", symbolStr)
			}
		}
	}
}

func (fb *FuturesBot) shouldTriggerTrail(pos *FuturesPosition, current udecimal.Decimal) bool {
	gapAllowed := udecimal.MustFromFloat64(fb.cfg.FuturesConfig.TrailingGap)
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

func (fb *FuturesBot) scanForEntries() int {
	scanned := 0
	skippedMaxPositions := 0
	skippedAlreadyOpen := 0
	skippedInsufficientHistory := 0
	analyzed := 0

	for _, symbol := range fb.trackedSymbols {
		symbolStr := symbol.String()
		scanned++

		if len(fb.positions) >= fb.cfg.FuturesConfig.MaxPositions {
			if fb.cfg.GeneralConfig.VerboseLogging && skippedMaxPositions == 0 {
				logger.DebugFmt("Max positions reached (%d/%d), skipping entry scan", len(fb.positions), fb.cfg.FuturesConfig.MaxPositions)
			}
			skippedMaxPositions++
			continue
		}
		if _, exists := fb.positions[symbolStr]; exists {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping %s: already has open position", symbolStr)
			}
			skippedAlreadyOpen++
			continue
		}

		history := fb.history[symbolStr]
		if len(history) < fb.cfg.FuturesConfig.LookbackPoints {
			if fb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping %s: insufficient history (%d/%d points)", symbolStr, len(history), fb.cfg.FuturesConfig.LookbackPoints)
			}
			skippedInsufficientHistory++
			continue
		}

		change, err := fb.analyzeMomentum(history)
		if err != nil {
			logger.ErrorFmt("Failed to analyze momentum for %s: %v", symbolStr, err)
			continue
		}

		analyzed++
		changePercent := change.Mul(udecimal.MustFromFloat64(100))
		threshold := udecimal.MustFromFloat64(fb.cfg.FuturesConfig.EntryChange)
		thresholdPercent := threshold.Mul(udecimal.MustFromFloat64(100))
		changeFloat := changePercent.InexactFloat64()
		thresholdFloat := thresholdPercent.InexactFloat64()

		// Always log momentum analysis for debugging
		logger.InfoFmt("Momentum analysis for %s: change=%.4f%%, threshold=±%.4f%%, history_points=%d",
			symbolStr, changeFloat, thresholdFloat, len(history))

		if change.Cmp(threshold) >= 0 {
			logger.InfoFmt("LONG signal detected for %s: momentum=%.4f%% >= threshold=%.4f%%", symbolStr, changeFloat, thresholdFloat)
			fb.openPosition(symbol, "LONG")
		} else if change.Cmp(threshold.Neg()) <= 0 {
			logger.InfoFmt("SHORT signal detected for %s: momentum=%.4f%% <= threshold=%.4f%%", symbolStr, changeFloat, -thresholdFloat)
			fb.openPosition(symbol, "SHORT")
		} else {
			logger.DebugFmt("No entry signal for %s: momentum=%.4f%% (threshold: ±%.4f%%)", symbolStr, changeFloat, thresholdFloat)
		}
	}

	if fb.cfg.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Entry scan complete: scanned=%d, analyzed=%d, skipped: max_positions=%d, already_open=%d, insufficient_history=%d",
			scanned, analyzed, skippedMaxPositions, skippedAlreadyOpen, skippedInsufficientHistory)
	}

	return scanned
}

func (fb *FuturesBot) analyzeMomentum(history []PricePoint) (udecimal.Decimal, error) {
	if len(history) < 2 {
		return udecimal.MustFromFloat64(0), fmt.Errorf("insufficient history")
	}

	// Use the configured lookback points, but ensure we have enough data
	lookback := fb.cfg.FuturesConfig.LookbackPoints
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
	if fb.cfg.GeneralConfig.VerboseLogging {
		firstPrice := first.InexactFloat64()
		lastPrice := last.InexactFloat64()
		changePercent := change.Mul(udecimal.MustFromFloat64(100)).InexactFloat64()
		logger.DebugFmt("Momentum analysis: first=%.8f, last=%.8f, change=%.4f%%, lookback=%d points",
			firstPrice, lastPrice, changePercent, lookback)
	}

	return change, nil
}

func (fb *FuturesBot) openPosition(symbol currency.Pair, side string) {
	symbolData, ok := fb.ob.Symbols.Get(symbol.String())
	if !ok {
		logger.WarningFmt("Symbol %s not found for futures entry", symbol.String())
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

	rawQty, err := udecimal.MustFromFloat64(fb.cfg.FuturesConfig.USDTPositionSize).Div(entryPrice)
	if err != nil {
		logger.ErrorFmt("Failed to derive raw quantity for %s: %v", symbol.String(), err)
		return
	}
	validatedQty, err := arbitrage.StepSizeQuantity(symbolData, rawQty)
	if err != nil {
		logger.ErrorFmt("Failed to apply step size for %s: %v", symbol.String(), err)
		return
	}

	position := &FuturesPosition{
		Symbol:      symbol,
		Side:        side,
		EntryPrice:  entryPrice,
		Quantity:    validatedQty,
		EntryTime:   time.Now(),
		Status:      "OPEN",
		PeakPrice:   entryPrice,
		TroughPrice: entryPrice,
		Notional:    entryPrice.Mul(validatedQty),
		Leverage:    fb.cfg.FuturesConfig.Leverage,
	}

	if !fb.cfg.GeneralConfig.SimulationMode {
		if err := fb.placeFuturesOrder(symbol, side, validatedQty); err != nil {
			logger.ErrorFmt("Live futures order failed for %s: %v", symbol.String(), err)
			return
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would open %s %s qty %s at %s", side, symbol.String(), validatedQty.String(), entryPrice.String())))
	}

	fb.positions[symbol.String()] = position

	// Log simulation trade
	if fb.cfg.GeneralConfig.SimulationMode && fb.simLogger != nil {
		fb.logSimulationTrade("OPEN", symbol.String(), position, entryPrice, udecimal.Zero, udecimal.Zero, "ENTRY", time.Duration(0))
	}

	logger.InfoFmt("Opened futures %s for %s: entry=%s qty=%s lev=%dx", side, symbol.String(), entryPrice.String(), validatedQty.String(), position.Leverage)
	fb.tradeLogger.Info().
		Str("action", "OPEN_FUTURES_POSITION").
		Str("symbol", symbol.String()).
		Str("side", side).
		Str("entry_price", entryPrice.String()).
		Str("quantity", validatedQty.String()).
		Int("leverage", position.Leverage).
		Time("entry_time", position.EntryTime).
		Msg("Futures position opened")
}

func (fb *FuturesBot) closePosition(symbolStr string, exitPrice udecimal.Decimal, reason string) {
	position, exists := fb.positions[symbolStr]
	if !exists || position.Status != "OPEN" {
		return
	}

	priceChange, err := priceChangeFromEntry(position, exitPrice)
	if err != nil {
		logger.ErrorFmt("Failed to calculate exit P&L for %s: %v", symbolStr, err)
		return
	}
	effectivePnL := priceChange.Mul(udecimal.MustFromFloat64(float64(position.Leverage)))
	profitAmount := effectivePnL.Mul(position.Notional)
	holdDuration := time.Since(position.EntryTime)

	exitSide := "SELL"
	if position.Side == "SHORT" {
		exitSide = "BUY"
	}

	if !fb.cfg.GeneralConfig.SimulationMode {
		if err := fb.placeFuturesOrder(position.Symbol, exitSide, position.Quantity); err != nil {
			logger.ErrorFmt("Failed to close futures %s: %v", symbolStr, err)
			return
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would close %s via %s %s at %s", symbolStr, exitSide, position.Quantity.String(), exitPrice.String())))
	}

	position.Status = "CLOSED"
	delete(fb.positions, symbolStr)

	// Create closed position record
	closedPosition := ClosedFuturesPosition{
		EntryPosition: *position,
		ClosingPrice:  exitPrice.StringFixed(8),
		ClosingTime:   time.Now(),
		Profit:        profitAmount.StringFixed(6),
		PnLPercent:    effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(4),
		Reason:        reason,
		HoldDuration:  holdDuration.Round(time.Second).String(),
	}

	// Store closed position
	if fb.closedPositions[symbolStr] == nil {
		fb.closedPositions[symbolStr] = make([]ClosedFuturesPosition, 0)
	}
	fb.closedPositions[symbolStr] = append(fb.closedPositions[symbolStr], closedPosition)

	// Update statistics (always track stats, regardless of simulation mode)
	fb.updateSimStats(profitAmount, effectivePnL, position.Notional, holdDuration)

	// Log simulation trade if in simulation mode
	if fb.cfg.GeneralConfig.SimulationMode && fb.simLogger != nil {
		fb.logSimulationTrade("CLOSE", symbolStr, position, exitPrice, effectivePnL, profitAmount, reason, holdDuration)
	}

	logger.InfoFmt("Closed futures %s: P&L=%s%% amount=%s reason=%s", symbolStr, effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(3), profitAmount.StringFixed(4), logger.ColorizeReason(reason))
	fb.tradeLogger.Info().
		Str("action", "CLOSE_FUTURES_POSITION").
		Str("symbol", symbolStr).
		Str("side", position.Side).
		Str("entry_price", position.EntryPrice.String()).
		Str("exit_price", exitPrice.String()).
		Str("pnl_percent", effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(3)).
		Str("pnl_amount", profitAmount.StringFixed(4)).
		Str("reason", reason).
		Time("exit_time", time.Now()).
		Msg("Futures position closed")
}

func (fb *FuturesBot) placeFuturesOrder(pair currency.Pair, side string, qty udecimal.Decimal) error {
	if fb.futuresClient == nil {
		return fmt.Errorf("futures client not configured")
	}
	symbolData, hasSymbol := fb.ob.Symbols.Get(pair.String())
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

	var sideType futures.SideType
	if side == "BUY" || side == "LONG" {
		sideType = futures.SideTypeBuy
	} else {
		sideType = futures.SideTypeSell
	}

	order, err := fb.futuresClient.NewCreateOrderService().Symbol(pair.String()).
		Quantity(formattedQty).
		Type(futures.OrderTypeMarket).
		Side(sideType).
		Do(context.Background())
	if err != nil {
		return fmt.Errorf("failed to place futures order: %w", err)
	}

	if fb.cfg.GeneralConfig.VerboseLogging {
		logger.InfoFmt("Futures order response: %+v", order)
	}
	logger.InfoFmt("Futures order placed for %s %s %s at market (lev %dx)", pair.String(), side, formattedQty, fb.cfg.FuturesConfig.Leverage)
	return nil
}

func priceChangeFromEntry(pos *FuturesPosition, current udecimal.Decimal) (udecimal.Decimal, error) {
	if pos.Side == "LONG" {
		return current.Sub(pos.EntryPrice).Div(pos.EntryPrice)
	}
	return pos.EntryPrice.Sub(current).Div(pos.EntryPrice)
}

func midPrice(book *arbitrage.BookTicker) (udecimal.Decimal, error) {
	bid, err := udecimal.Parse(book.BidPrice)
	if err != nil {
		return udecimal.MustFromFloat64(0), err
	}
	ask, err := udecimal.Parse(book.AskPrice)
	if err != nil {
		return udecimal.MustFromFloat64(0), err
	}
	total := bid.Add(ask)
	mid, err := total.Div(udecimal.MustFromFloat64(2))
	if err != nil {
		return udecimal.MustFromFloat64(0), err
	}
	return mid, nil
}

func midPriceFromPoint(pp PricePoint) (udecimal.Decimal, error) {
	bid, err := udecimal.Parse(pp.BidPrice)
	if err != nil {
		return udecimal.MustFromFloat64(0), err
	}
	ask, err := udecimal.Parse(pp.AskPrice)
	if err != nil {
		return udecimal.Zero, err
	}
	total := bid.Add(ask)
	mid, err := total.Div(udecimal.MustFromFloat64(2))
	if err != nil {
		return udecimal.Zero, err
	}
	return mid, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (fb *FuturesBot) logSimulationTrade(action, symbolStr string, position *FuturesPosition, price udecimal.Decimal, pnlPercent, pnlAmount udecimal.Decimal, reason string, holdDuration time.Duration) {
	if fb.simLogger == nil {
		return
	}

	entry := fb.simLogger.Info().
		Str("action", action).
		Str("symbol", symbolStr).
		Str("side", position.Side).
		Str("entry_price", position.EntryPrice.String()).
		Str("quantity", position.Quantity.String()).
		Int("leverage", position.Leverage).
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

	entry.Msg("FUTURES_SIMULATION_TRADE")
}

func (fb *FuturesBot) updateSimStats(pnlAmount, pnlPercent, volume udecimal.Decimal, holdDuration time.Duration) {
	fb.simStats.mu.Lock()
	defer fb.simStats.mu.Unlock()

	fb.simStats.TotalTrades++
	fb.simStats.TotalPnL = fb.simStats.TotalPnL.Add(pnlAmount)
	fb.simStats.TotalVolume = fb.simStats.TotalVolume.Add(volume)
	fb.simStats.LastTradeTime = time.Now()

	// Update best/worst trade
	if pnlAmount.Cmp(fb.simStats.BestTradePnL) > 0 {
		fb.simStats.BestTradePnL = pnlAmount
	}
	if pnlAmount.Cmp(fb.simStats.WorstTradePnL) < 0 {
		fb.simStats.WorstTradePnL = pnlAmount
	}

	// Count wins/losses
	if pnlAmount.Cmp(udecimal.Zero) > 0 {
		fb.simStats.WinningTrades++
	} else if pnlAmount.Cmp(udecimal.Zero) < 0 {
		fb.simStats.LosingTrades++
	}

	// Calculate average P&L percent
	if fb.simStats.TotalTrades > 0 && !fb.simStats.TotalVolume.IsZero() {
		if avgPnLPercent, err := fb.simStats.TotalPnL.Div(fb.simStats.TotalVolume); err == nil {
			fb.simStats.TotalPnLPercent = avgPnLPercent
		}
	}

	// Log periodic summary every 10 trades
	if fb.simStats.TotalTrades%10 == 0 {
		fb.logSimulationSummary()
	}
}

func (fb *FuturesBot) logSimulationSummary() {
	if fb.simLogger == nil {
		return
	}

	fb.simStats.mu.RLock()
	defer fb.simStats.mu.RUnlock()

	winRate := udecimal.Zero
	if fb.simStats.TotalTrades > 0 {
		if wr, err := udecimal.MustFromFloat64(float64(fb.simStats.WinningTrades)).Div(udecimal.MustFromFloat64(float64(fb.simStats.TotalTrades))); err == nil {
			winRate = wr
		}
	}

	avgPnL := udecimal.Zero
	if fb.simStats.TotalTrades > 0 {
		if avg, err := fb.simStats.TotalPnL.Div(udecimal.MustFromFloat64(float64(fb.simStats.TotalTrades))); err == nil {
			avgPnL = avg
		}
	}

	runTime := time.Since(fb.simStats.StartTime)

	fb.simLogger.Info().
		Str("summary_type", "PERIODIC").
		Int("total_trades", fb.simStats.TotalTrades).
		Int("winning_trades", fb.simStats.WinningTrades).
		Int("losing_trades", fb.simStats.LosingTrades).
		Str("win_rate_percent", winRate.Mul(udecimal.MustFromFloat64(100)).StringFixed(2)).
		Str("total_pnl_usdt", fb.simStats.TotalPnL.StringFixed(4)).
		Str("total_pnl_percent", fb.simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)).
		Str("avg_pnl_per_trade_usdt", avgPnL.StringFixed(4)).
		Str("best_trade_usdt", fb.simStats.BestTradePnL.StringFixed(4)).
		Str("worst_trade_usdt", fb.simStats.WorstTradePnL.StringFixed(4)).
		Str("total_volume_usdt", fb.simStats.TotalVolume.StringFixed(2)).
		Str("runtime", runTime.Round(time.Second).String()).
		Time("last_trade_time", fb.simStats.LastTradeTime).
		Msg("FUTURES_SIMULATION_SUMMARY")
}

func (fb *FuturesBot) LogFinalSimulationSummary() {
	if !fb.cfg.GeneralConfig.SimulationMode || fb.simLogger == nil {
		return
	}

	fb.simStats.mu.RLock()
	defer fb.simStats.mu.RUnlock()

	winRate := udecimal.Zero
	if fb.simStats.TotalTrades > 0 {
		if wr, err := udecimal.MustFromFloat64(float64(fb.simStats.WinningTrades)).Div(udecimal.MustFromFloat64(float64(fb.simStats.TotalTrades))); err == nil {
			winRate = wr
		}
	}

	avgPnL := udecimal.Zero
	if fb.simStats.TotalTrades > 0 {
		if avg, err := fb.simStats.TotalPnL.Div(udecimal.MustFromFloat64(float64(fb.simStats.TotalTrades))); err == nil {
			avgPnL = avg
		}
	}

	runTime := time.Since(fb.simStats.StartTime)

	fb.simLogger.Info().
		Str("summary_type", "FINAL").
		Int("total_trades", fb.simStats.TotalTrades).
		Int("winning_trades", fb.simStats.WinningTrades).
		Int("losing_trades", fb.simStats.LosingTrades).
		Str("win_rate_percent", winRate.Mul(udecimal.MustFromFloat64(100)).StringFixed(2)).
		Str("total_pnl_usdt", fb.simStats.TotalPnL.StringFixed(4)).
		Str("total_pnl_percent", fb.simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)).
		Str("avg_pnl_per_trade_usdt", avgPnL.StringFixed(4)).
		Str("best_trade_usdt", fb.simStats.BestTradePnL.StringFixed(4)).
		Str("worst_trade_usdt", fb.simStats.WorstTradePnL.StringFixed(4)).
		Str("total_volume_usdt", fb.simStats.TotalVolume.StringFixed(2)).
		Str("runtime", runTime.Round(time.Second).String()).
		Time("start_time", fb.simStats.StartTime).
		Time("end_time", time.Now()).
		Msg("FUTURES_SIMULATION_FINAL_SUMMARY")

	logger.InfoFmt("=== FUTURES SIMULATION FINAL SUMMARY ===")
	logger.InfoFmt("Total Trades: %d (Wins: %d, Losses: %d, Win Rate: %.2f%%)",
		fb.simStats.TotalTrades, fb.simStats.WinningTrades, fb.simStats.LosingTrades, winRate.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())

	totalPnLPercent := udecimal.Zero
	if !fb.simStats.TotalVolume.IsZero() {
		if pnlPct, err := fb.simStats.TotalPnL.Div(fb.simStats.TotalVolume); err == nil {
			totalPnLPercent = pnlPct
		}
	}

	logger.InfoFmt("Total P&L: %s USDT (%.4f%%)", fb.simStats.TotalPnL.StringFixed(4), totalPnLPercent.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
	logger.InfoFmt("Average P&L per Trade: %s USDT", avgPnL.StringFixed(4))
	logger.InfoFmt("Best Trade: %s USDT | Worst Trade: %s USDT", fb.simStats.BestTradePnL.StringFixed(4), fb.simStats.WorstTradePnL.StringFixed(4))
	logger.InfoFmt("Total Volume: %s USDT", fb.simStats.TotalVolume.StringFixed(2))
	logger.InfoFmt("Runtime: %v", runTime.Round(time.Second))
	logger.InfoFmt("========================================")
}

// ClosedPositions returns the closed positions map (for statistics)
func (fb *FuturesBot) ClosedPositions() map[string][]ClosedFuturesPosition {
	fb.mu.RLock()
	defer fb.mu.RUnlock()
	return fb.closedPositions
}

// GetSimStats returns a copy of the simulation statistics
func (fb *FuturesBot) GetSimStats() *SimulationStats {
	fb.mu.RLock()
	defer fb.mu.RUnlock()

	if fb.simStats == nil {
		return nil
	}

	fb.simStats.mu.RLock()
	defer fb.simStats.mu.RUnlock()

	// Return a copy to avoid race conditions
	return &SimulationStats{
		TotalTrades:     fb.simStats.TotalTrades,
		WinningTrades:   fb.simStats.WinningTrades,
		LosingTrades:    fb.simStats.LosingTrades,
		TotalPnL:        fb.simStats.TotalPnL,
		TotalPnLPercent: fb.simStats.TotalPnLPercent,
		BestTradePnL:    fb.simStats.BestTradePnL,
		WorstTradePnL:   fb.simStats.WorstTradePnL,
		TotalVolume:     fb.simStats.TotalVolume,
		StartTime:       fb.simStats.StartTime,
		LastTradeTime:   fb.simStats.LastTradeTime,
	}
}
