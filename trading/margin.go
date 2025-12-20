package trading

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

type ClosedMarginPosition struct {
	EntryPosition MarginPosition
	ClosingPrice  string
	ClosingTime   time.Time
	Profit        string
	PnLPercent    string
	Reason        string
	HoldDuration  string
}

type MarginBot struct {
	ob                 *arbitrage.Orderbook
	cfg                *utils.Config
	tradeLogger        *zerolog.Logger
	simLogger          *zerolog.Logger
	client             *binance.Client
	trackedSymbols     []currency.Pair
	positions          map[string]*MarginPosition
	closedPositions    map[string][]ClosedMarginPosition
	history            map[string][]PricePoint
	simStats           *SimulationStats
	unsupportedSymbols map[string]string    // Maps symbol to reason (e.g., "MARGIN_NOT_ALLOWED", "ISOLATED_ACCOUNT_MISSING", "MARGIN_ACCOUNT_MISSING")
	cooldownUntil      map[string]time.Time // Maps symbol to cooldown expiration time for temporary errors (e.g., -3045)
	mu                 sync.RWMutex
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
	symbolsStr := strings.Join(func() []string {
		symbols := make([]string, len(tracked))
		for i, p := range tracked {
			symbols[i] = p.String()
		}
		return symbols
	}(), ", ")
	logger.InfoFmt("Tracked symbols: %s", symbolsStr)

	return &MarginBot{
		ob:                 ob,
		cfg:                cfg,
		tradeLogger:        tradeLogger,
		simLogger:          simLogger,
		client:             client,
		trackedSymbols:     tracked,
		positions:          positions,
		closedPositions:    make(map[string][]ClosedMarginPosition),
		history:            history,
		simStats:           simStats,
		unsupportedSymbols: make(map[string]string),
		cooldownUntil:      make(map[string]time.Time),
	}
}

// Positions returns the positions map (for persistence)
func (mb *MarginBot) Positions() map[string]*MarginPosition {
	return mb.positions
}

// Mu returns the mutex (for thread-safe access)
func (mb *MarginBot) Mu() *sync.RWMutex {
	return &mb.mu
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

	for {
		select {
		case <-ctx.Done():
			logger.InfoFmt("Margin bot loop stopped after %d cycles", cycleCount)
			return
		case <-ticker.C:
			cycleCount++
			mb.processCycle()
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
		effectivePnL.Mul(udecimal.MustFromFloat64(100))

		if mb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Position %s %s: entry=%s current=%s P&L=%s%% hold=%v",
				symbolStr, position.Side, position.EntryPrice.String(), currentPrice.String(),
				logger.ColorizePnl(effectivePnL), holdDuration.Round(time.Second))
		}

		// Stop loss
		stopLossThreshold := udecimal.MustFromFloat64(-mb.cfg.MarginConfig.StopLoss)
		if effectivePnL.Cmp(stopLossThreshold) <= 0 {
			logger.InfoFmt("%s for %s: P&L=%s%% <= %.4f%%", logger.BrightRed("Stop loss triggered"), symbolStr, logger.ColorizePnl(effectivePnL), stopLossThreshold.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
			mb.closePosition(symbolStr, currentPrice, "STOP_LOSS")
			continue
		}

		// Time-based exit
		// Only close on timeout if position is profitable (P&L > 0)
		// If losing money, keep it open unless stop loss is reached
		maxHoldTime := time.Duration(mb.cfg.NormalConfig.MaxHoldTime) * time.Minute
		if holdDuration > maxHoldTime {
			if effectivePnL.Cmp(udecimal.Zero) >= 0 {
				logger.InfoFmt("%s for %s: P&L=%s%%. Closing position.", logger.Blue("Max hold time reached"), symbolStr, logger.ColorizePnl(effectivePnL))
				mb.closePosition(symbolStr, currentPrice, "TIMEOUT")
			} else {
				if mb.cfg.GeneralConfig.VerboseLogging {
					logger.DebugFmt("Max hold time reached for %s but position is unprofitable (P&L=%s). Keeping position open until stop loss.", symbolStr, logger.ColorizePnl(effectivePnL))
				}
			}
			continue
		}

		// Trailing stop once target reached
		profitTarget := udecimal.MustFromFloat64(mb.cfg.MarginConfig.TrailingStart)
		if effectivePnL.Cmp(profitTarget) >= 0 {
			if mb.shouldTriggerTrail(position, currentPrice) {
				logger.InfoFmt("%s for %s: P&L=%s%% >= %.4f%%", logger.BrightGreen("Trailing exit triggered"), symbolStr, logger.ColorizePnl(effectivePnL), profitTarget.Mul(udecimal.MustFromFloat64(100)).InexactFloat64())
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
		if mb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Momentum analysis for %s: change=%.4f%%, threshold=±%.4f%%, history_points=%d",
				symbolStr, changeFloat, thresholdFloat, len(history))
		}

		if change.Cmp(threshold) >= 0 {
			logger.InfoFmt("%s signal detected for %s: momentum=%.4f%% >= threshold=%.4f%%", logger.BgGreen(" LONG "), symbolStr, changeFloat, thresholdFloat)
			mb.openPosition(symbol, "LONG")
		} else if change.Cmp(threshold.Neg()) <= 0 {
			logger.InfoFmt("%s signal detected for %s: momentum=%.4f%% <= threshold=%.4f%%", logger.BgRed(" SHORT "), symbolStr, changeFloat, -thresholdFloat)
			mb.openPosition(symbol, "SHORT")
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
	lookback := min(mb.cfg.MarginConfig.LookbackPoints, len(history))
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

// isMarginErrorPermanent checks if an error indicates that margin trading is permanently unavailable for this symbol
func (mb *MarginBot) isMarginErrorPermanent(err error) (bool, string) {
	errStr := err.Error()

	// Error code -3021: Margin account are not allowed to trade this trading pair
	if strings.Contains(errStr, "-3021") || strings.Contains(errStr, "not allowed to trade this trading pair") {
		return true, "MARGIN_NOT_ALLOWED"
	}

	// Error code -11001: Isolated margin account does not exist
	if strings.Contains(errStr, "-11001") || strings.Contains(errStr, "Isolated margin account does not exist") {
		return true, "ISOLATED_ACCOUNT_MISSING"
	}

	// Error code -3003: Margin account does not exist
	if strings.Contains(errStr, "-3003") || strings.Contains(errStr, "Margin account does not exist") {
		return true, "MARGIN_ACCOUNT_MISSING"
	}

	return false, ""
}

func (mb *MarginBot) openPosition(symbol currency.Pair, side string) {
	// Add panic recovery to prevent crashes
	defer func() {
		if r := recover(); r != nil {
			logger.ErrorFmt("Panic recovered in openPosition for %s %s: %v", side, symbol.String(), r)
		}
	}()

	symbolStr := symbol.String()

	// Check if this symbol is already marked as unsupported
	// Note: This function is called from processCycle() which already holds mb.mu.Lock(),
	// so we can access unsupportedSymbols directly without additional locking
	if reason, exists := mb.unsupportedSymbols[symbolStr]; exists {
		if mb.cfg.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Skipping %s %s: symbol marked as unsupported (%s)", side, symbolStr, reason)
		}
		return
	}

	// Check if this symbol is in cooldown (for temporary errors like -3045)
	if cooldownExpiry, exists := mb.cooldownUntil[symbolStr]; exists {
		if time.Now().Before(cooldownExpiry) {
			remaining := time.Until(cooldownExpiry).Round(time.Second)
			if mb.cfg.GeneralConfig.VerboseLogging {
				logger.DebugFmt("Skipping %s %s: symbol in cooldown (remaining: %v)", side, symbolStr, remaining)
			}
			return
		}
		// Cooldown expired, remove it
		delete(mb.cooldownUntil, symbolStr)
	}

	symbolData, ok := mb.ob.Symbols.Get(symbolStr)
	if !ok {
		logger.WarningFmt("Symbol %s not found for margin entry", symbolStr)
		return
	}

	book := symbolData.GetBookTicker()
	if book == nil {
		return
	}

	entryPrice, err := midPrice(book)
	if err != nil {
		logger.ErrorFmt("Failed to parse entry price for %s: %v", symbolStr, err)
		return
	}

	// Calculate quantity: for both LONG and SHORT, we want to invest USDCPositionSize worth
	// LONG: buy base asset with USDC, quantity = USDC / price
	// SHORT: borrow and sell base asset for USDC, quantity = USDC / price (same calculation)
	rawQty, err := udecimal.MustFromFloat64(mb.cfg.MarginConfig.USDTPositionSize).Div(entryPrice)
	if err != nil {
		logger.ErrorFmt("Failed to derive raw quantity for %s: %v", symbolStr, err)
		return
	}
	validatedQty, err := arbitrage.StepSizeQuantity(symbolData, rawQty)
	if err != nil {
		logger.ErrorFmt("Failed to apply step size for %s: %v", symbolStr, err)
		return
	}

	// In live mode, place the order first and only create position if successful
	if !mb.cfg.GeneralConfig.SimulationMode {
		// Use regular spot orders for LONG positions (buy with own funds)
		// Use margin orders only for SHORT positions (borrow to sell)
		switch side {
		case "LONG":
			if err := mb.placeSpotOrder(symbol, "BUY", validatedQty); err != nil {
				logger.ErrorFmt("Live spot order failed for %s: %v", symbolStr, err)
				return
			}
		case "SHORT":
			orderSucceeded := false
			if err := mb.placeMarginOrder(symbol, "SHORT", validatedQty); err != nil {
				errStr := err.Error()

				// Check if this is an unmarshaling error (order likely succeeded)
				if strings.Contains(errStr, "UNMARSHAL_ERROR") {
					logger.WarningFmt("Margin order response parsing failed for %s (order likely succeeded on Binance): %v", symbolStr, err)
					logger.WarningFmt("This is a known issue where Binance returns numeric fields as strings.")
					logger.InfoFmt("Creating position anyway to track the order that was placed on Binance.")
					orderSucceeded = true
				} else if strings.Contains(errStr, "-3006") || strings.Contains(errStr, "borrow amount has exceed maximum borrow amount") || strings.Contains(errStr, "maximum borrow amount") {
					// Check if this is a borrowing limit error (temporary, not permanent)
					logger.WarningFmt("Cannot open SHORT position for %s: Maximum borrowing limit reached.", symbolStr)
					logger.WarningFmt("This is a temporary condition. Possible solutions:")
					logger.WarningFmt("  1. Wait for existing positions to close (freeing up borrowing capacity)")
					logger.WarningFmt("  2. Reduce USDTPositionSize in config to use smaller position sizes")
					logger.WarningFmt("  3. Add more collateral to your margin account")
					logger.WarningFmt("  4. Close some existing SHORT positions manually")
					logger.WarningFmt("The bot will retry on the next cycle when capacity becomes available.")
					return
				} else if strings.Contains(errStr, "-3045") || strings.Contains(errStr, "does not have enough asset now") {
					// Check if this is a temporary "insufficient asset" error
					// Set a 5-minute cooldown before retrying
					cooldownDuration := 5 * time.Minute
					cooldownExpiry := time.Now().Add(cooldownDuration)
					mb.cooldownUntil[symbolStr] = cooldownExpiry
					logger.WarningFmt("Cannot open SHORT position for %s: Binance does not have enough asset available (error -3045).", symbolStr)
					logger.WarningFmt("Setting cooldown for %v. Will retry after %v.", symbolStr, cooldownExpiry.Format("15:04:05"))
					return
				} else if isPermanent, reason := mb.isMarginErrorPermanent(err); isPermanent {
					// Check if this is a permanent error (margin not allowed for this symbol)
					// Note: This function is called from processCycle() which already holds mb.mu.Lock(),
					// so we can update unsupportedSymbols directly without additional locking
					mb.unsupportedSymbols[symbolStr] = reason

					var errorMsg string
					switch reason {
					case "MARGIN_NOT_ALLOWED":
						errorMsg = fmt.Sprintf("Margin trading is not enabled for %s on your Binance account. This symbol will be skipped for future SHORT positions.", symbolStr)
					case "ISOLATED_ACCOUNT_MISSING":
						errorMsg = fmt.Sprintf("Isolated margin account does not exist for %s. Enable isolated margin for this symbol on Binance, or switch to CROSS margin mode. This symbol will be skipped for future SHORT positions.", symbolStr)
					case "MARGIN_ACCOUNT_MISSING":
						errorMsg = fmt.Sprintf("Margin account does not exist for %s. Enable margin trading on your Binance account. This symbol will be skipped for future SHORT positions.", symbolStr)
					default:
						errorMsg = fmt.Sprintf("Margin trading unavailable for %s: %s. This symbol will be skipped.", symbolStr, reason)
					}
					logger.WarningFmt("%s", errorMsg)
					return
				} else {
					logger.ErrorFmt("Live margin order failed for %s: %v", symbolStr, err)
					return
				}
			} else {
				orderSucceeded = true
			}

			// Create position if order succeeded (including unmarshal errors where order likely succeeded)
			if !orderSucceeded {
				return
			}
		}
		// Only proceed to create position if API call succeeded
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would open %s %s qty %s at %s", side, symbolStr, validatedQty.String(), entryPrice.String())))
	}

	// Create position object only after successful order placement (or in simulation mode)
	borrowedQty := udecimal.Zero
	if side == "SHORT" {
		// For SHORT positions, we borrowed the base asset quantity
		borrowedQty = validatedQty
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
		BorrowedQty: borrowedQty,
	}

	mb.positions[symbolStr] = position

	// Log simulation trade if in simulation mode
	if mb.cfg.GeneralConfig.SimulationMode && mb.simLogger != nil {
		mb.logSimulationTrade("OPEN", symbol.String(), position, entryPrice, udecimal.Zero, udecimal.Zero, "ENTRY", time.Duration(0))
	}

	logger.InfoFmt("%s for %s: entry=%s qty=%s", logger.BrightGreen("Opened margin ")+logger.ColorizeSide(side), symbol.String(), entryPrice.String(), validatedQty.String())
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

// getAccountBalance retrieves the free balance for a specific asset
func (mb *MarginBot) getAccountBalance(asset string) (udecimal.Decimal, error) {
	if mb.client == nil {
		return udecimal.Zero, fmt.Errorf("client not configured")
	}

	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	accountService := mb.client.NewGetAccountService()
	res, err := accountService.Do(ctx)
	if err != nil {
		return udecimal.Zero, fmt.Errorf("failed to get account: %w", err)
	}

	for _, balance := range res.Balances {
		if balance.Asset == asset {
			balanceDecimal, err := udecimal.Parse(balance.Free)
			if err != nil {
				return udecimal.Zero, fmt.Errorf("failed to parse balance for %s: %w", asset, err)
			}
			return balanceDecimal, nil
		}
	}

	return udecimal.Zero, nil // Asset not found, return zero balance
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

	// Determine the actual quantity to use for closing
	closeQuantity := position.Quantity

	if !mb.cfg.GeneralConfig.SimulationMode {
		// For LONG positions, fetch actual balance and use minimum of stored quantity and available balance
		// This prevents trying to sell more than we actually own due to fees/rounding
		if position.Side == "LONG" {
			actualBalance, err := mb.getAccountBalance(position.Symbol.Base.Symbol)
			if err != nil {
				logger.WarningFmt("Failed to get account balance for %s when closing %s: %v. Using stored quantity.", position.Symbol.Base.Symbol, symbolStr, err)
			} else {
				// Use the minimum of stored quantity and actual balance
				// This ensures we don't try to sell more than we have
				if actualBalance.Cmp(closeQuantity) < 0 {
					logger.InfoFmt("Actual balance (%s) is less than stored quantity (%s) for %s. Using actual balance.", actualBalance.String(), closeQuantity.String(), symbolStr)
					closeQuantity = actualBalance
				}

				// Apply step size rounding to ensure valid quantity
				// StepSizeQuantity always rounds down, so result will be <= input
				// Since we've already ensured closeQuantity <= actualBalance, the rounded result will also be <= actualBalance
				symbolData, ok := mb.ob.Symbols.Get(symbolStr)
				if ok {
					roundedQty, err := arbitrage.StepSizeQuantity(symbolData, closeQuantity)
					if err != nil {
						logger.WarningFmt("Failed to apply step size rounding for %s: %v. Using unrounded quantity.", symbolStr, err)
					} else {
						// StepSizeQuantity rounds down, so roundedQty <= closeQuantity <= actualBalance
						// No need to check if roundedQty > actualBalance as it's impossible
						closeQuantity = roundedQty
					}
				}

				// Final safety check: ensure we're not trying to sell zero or negative
				if closeQuantity.Cmp(udecimal.Zero) <= 0 {
					logger.ErrorFmt("Cannot close %s: calculated quantity (%s) is zero or negative", symbolStr, closeQuantity.String())
					return
				}
			}
		}

		// Use regular spot orders for closing LONG positions
		// Use margin orders with AUTO_REPAY for closing SHORT positions
		switch position.Side {
		case "LONG":
			if err := mb.placeSpotOrder(position.Symbol, exitSide, closeQuantity); err != nil {
				logger.ErrorFmt("Failed to close LONG position %s: %v", symbolStr, err)
				return
			}
		case "SHORT":
			// Use margin order with AUTO_REPAY to buy back and automatically repay borrowed assets
			if err := mb.placeMarginOrderWithRepay(position.Symbol, exitSide, closeQuantity, true); err != nil {
				logger.ErrorFmt("Failed to close SHORT position %s: %v", symbolStr, err)
				return
			}
		}
	} else {
		logger.InfoFmt("%s %s", logger.Yellow("[SIMULATION MODE]"), logger.Italic(fmt.Sprintf("Would close %s via %s %s at %s", symbolStr, exitSide, closeQuantity.String(), exitPrice.String())))
	}

	position.Status = "CLOSED"
	delete(mb.positions, symbolStr)

	// Create closed position record
	closedPosition := ClosedMarginPosition{
		EntryPosition: *position,
		ClosingPrice:  exitPrice.StringFixed(8),
		ClosingTime:   time.Now(),
		Profit:        profitAmount.StringFixed(6),
		PnLPercent:    effectivePnL.Mul(udecimal.MustFromFloat64(100)).StringFixed(4),
		Reason:        reason,
		HoldDuration:  holdDuration.Round(time.Second).String(),
	}

	// Store closed position
	if mb.closedPositions[symbolStr] == nil {
		mb.closedPositions[symbolStr] = make([]ClosedMarginPosition, 0)
	}
	mb.closedPositions[symbolStr] = append(mb.closedPositions[symbolStr], closedPosition)

	// Update statistics (always track stats, regardless of simulation mode)
	mb.updateSimStats(profitAmount, position.Notional)

	// Log simulation trade if in simulation mode
	if mb.cfg.GeneralConfig.SimulationMode && mb.simLogger != nil {
		mb.logSimulationTrade("CLOSE", symbolStr, position, exitPrice, effectivePnL, profitAmount, reason, holdDuration)
	}

	logger.InfoFmt("%s for %s: P&L=%s%% amount=%s reason=%s", logger.BrightMagenta("Closed margin ")+logger.ColorizeSide(position.Side), symbolStr, logger.ColorizePnl(effectivePnL.Mul(udecimal.MustFromFloat64(100))), profitAmount.StringFixed(4), logger.ColorizeReason(reason))
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

	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	order, err := orderService.Do(ctx)
	if err != nil {
		// Check if this is a JSON unmarshaling error for marginBuyBorrowAmount
		// Binance sometimes returns numeric fields as strings, causing unmarshaling to fail
		// The order might have actually succeeded on Binance's side
		errStr := err.Error()
		if strings.Contains(errStr, "marginBuyBorrowAmount") && strings.Contains(errStr, "cannot unmarshal string") {
			// This is a known issue with Binance API responses
			// The order may have succeeded, but we can't verify without the response
			// For closing positions, we'll log a warning but treat it as potentially successful
			logger.WarningFmt("Margin order response parsing failed for %s %s (order may have succeeded): %v", pair.String(), side, err)
			logger.WarningFmt("This is a known issue where Binance returns numeric fields as strings.")
			// Return nil to allow the position closure to proceed
			// The order likely succeeded on Binance's side
			return nil
		}
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
	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	order, err := mb.client.NewCreateOrderService().Symbol(pair.String()).
		Quantity(formattedQtyFloat).Type("MARKET").Side(side).Do(ctx)
	if err != nil {
		return fmt.Errorf("failed to place spot order: %w", err)
	}

	if mb.cfg.GeneralConfig.VerboseLogging {
		logger.InfoFmt("Spot order response: %+v", order)
	}
	logger.InfoFmt("Spot order placed for %s %s %s at market", pair.String(), logger.ColorizeSide(side), formattedQty)
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

	// Add timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	order, err := orderService.Do(ctx)
	if err != nil {
		// Check if this is a JSON unmarshaling error for marginBuyBorrowAmount
		// Binance sometimes returns numeric fields as strings, causing unmarshaling to fail
		// The order might have actually succeeded on Binance's side
		errStr := err.Error()
		if strings.Contains(errStr, "marginBuyBorrowAmount") && strings.Contains(errStr, "cannot unmarshal string") {
			// This is a known issue with Binance API responses
			// The order may have succeeded, but we can't verify without the response
			// Return a special error type so the caller can handle it appropriately
			return fmt.Errorf("UNMARSHAL_ERROR: margin order response parsing failed (order may have succeeded): %w", err)
		}
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

func (mb *MarginBot) updateSimStats(pnlAmount, volume udecimal.Decimal) {
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

// ClosedPositions returns the closed positions map (for statistics)
func (mb *MarginBot) ClosedPositions() map[string][]ClosedMarginPosition {
	mb.mu.RLock()
	defer mb.mu.RUnlock()
	return mb.closedPositions
}

// GetSimStats returns a copy of the simulation statistics
func (mb *MarginBot) GetSimStats() *SimulationStats {
	mb.mu.RLock()
	defer mb.mu.RUnlock()

	if mb.simStats == nil {
		return nil
	}

	mb.simStats.mu.RLock()
	defer mb.simStats.mu.RUnlock()

	// Return a copy to avoid race conditions
	return &SimulationStats{
		TotalTrades:     mb.simStats.TotalTrades,
		WinningTrades:   mb.simStats.WinningTrades,
		LosingTrades:    mb.simStats.LosingTrades,
		TotalPnL:        mb.simStats.TotalPnL,
		TotalPnLPercent: mb.simStats.TotalPnLPercent,
		BestTradePnL:    mb.simStats.BestTradePnL,
		WorstTradePnL:   mb.simStats.WorstTradePnL,
		TotalVolume:     mb.simStats.TotalVolume,
		StartTime:       mb.simStats.StartTime,
		LastTradeTime:   mb.simStats.LastTradeTime,
	}
}
