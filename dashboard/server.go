package dashboard

import (
	"arbtribot/arbitrage"
	"arbtribot/grid"
	"arbtribot/logger"
	"arbtribot/trading"
	"arbtribot/utils"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	futures "github.com/adshao/go-binance/v2/futures"
	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
)

// Server handles the web dashboard HTTP server
type Server struct {
	port          string
	cfg           *utils.Config
	orderBook     *arbitrage.Orderbook
	tradingBot    *trading.TradingBot
	marginBot     *trading.MarginBot
	futuresBot    *trading.FuturesBot
	gridBot       *grid.GridTradingBot
	client        *binance.Client
	futuresClient *futures.Client
	startTime     time.Time
	mu            sync.RWMutex
}

// NewServer creates a new dashboard server
func NewServer(port string, cfg *utils.Config, orderBook *arbitrage.Orderbook) *Server {
	return &Server{
		port:      port,
		cfg:       cfg,
		orderBook: orderBook,
		startTime: time.Now(),
	}
}

// SetBots sets the active trading bots
func (s *Server) SetBots(tradingBot *trading.TradingBot, marginBot *trading.MarginBot, futuresBot *trading.FuturesBot, gridBot *grid.GridTradingBot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tradingBot = tradingBot
	s.marginBot = marginBot
	s.futuresBot = futuresBot
	s.gridBot = gridBot
}

// SetClients sets the Binance clients
func (s *Server) SetClients(client *binance.Client, futuresClient *futures.Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.client = client
	s.futuresClient = futuresClient
}

// Start starts the HTTP server
func (s *Server) Start() error {
	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/positions", s.handlePositions)
	mux.HandleFunc("/api/stats", s.handleStats)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/recent-trades", s.handleRecentTrades)
	mux.HandleFunc("/api/orderbook", s.handleOrderBook)
	mux.HandleFunc("/api/close-position", s.handleClosePosition)

	// Serve static files (dashboard HTML/CSS/JS)
	mux.HandleFunc("/", s.handleDashboard)

	addr := fmt.Sprintf(":%s", s.port)
	logger.InfoFmt("%s %s", logger.BrightGreen("Dashboard server starting on"), logger.Italic(logger.Blue("http://localhost:"+s.port)))
	return http.ListenAndServe(addr, mux)
}

// handleDashboard serves the main dashboard HTML
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(dashboardHTML))
}

// handleStatus returns the current bot status
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	status := map[string]interface{}{
		"running":        true,
		"uptime":         time.Since(s.startTime).String(),
		"startTime":      s.startTime.Format(time.RFC3339),
		"tradingMode":    s.cfg.GeneralConfig.TradingMode,
		"simulationMode": s.cfg.GeneralConfig.SimulationMode,
		"futures":        s.cfg.GeneralConfig.Futures,
		"orderBookSize":  s.orderBook.Symbols.Count(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// handlePositions returns current open positions
func (s *Server) handlePositions(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	positions := make([]map[string]interface{}, 0)

	switch s.cfg.GeneralConfig.TradingMode {
	case "margin":
		if s.marginBot != nil {
			botPositions := s.marginBot.Positions()
			for symbol, pos := range botPositions {
				currentPrice := s.getCurrentPrice(symbol, pos.Side)
				unrealizedPnL := s.calculateUnrealizedPnL(pos.EntryPrice, currentPrice, pos.Quantity, pos.Side)
				unrealizedPnLPercent := s.calculateUnrealizedPnLPercent(pos.EntryPrice, currentPrice, pos.Side)

				positions = append(positions, map[string]interface{}{
					"symbol":               symbol,
					"side":                 pos.Side,
					"entryPrice":           pos.EntryPrice.StringFixed(8),
					"currentPrice":         currentPrice.StringFixed(8),
					"quantity":             pos.Quantity.StringFixed(8),
					"notional":             pos.Notional.StringFixed(2),
					"entryTime":            pos.EntryTime.Format(time.RFC3339),
					"holdDuration":         time.Since(pos.EntryTime).Round(time.Second).String(),
					"unrealizedPnL":        unrealizedPnL.StringFixed(4),
					"unrealizedPnLPercent": unrealizedPnLPercent.StringFixed(4),
					"status":               pos.Status,
				})
			}
		}
	case "futures":
		if s.futuresBot != nil {
			botPositions := s.futuresBot.Positions()
			for symbol, pos := range botPositions {
				currentPrice := s.getCurrentPrice(symbol, pos.Side)
				unrealizedPnL := s.calculateUnrealizedPnL(pos.EntryPrice, currentPrice, pos.Quantity, pos.Side)
				unrealizedPnLPercent := s.calculateUnrealizedPnLPercent(pos.EntryPrice, currentPrice, pos.Side)

				positions = append(positions, map[string]interface{}{
					"symbol":               symbol,
					"side":                 pos.Side,
					"entryPrice":           pos.EntryPrice.StringFixed(8),
					"currentPrice":         currentPrice.StringFixed(8),
					"quantity":             pos.Quantity.StringFixed(8),
					"notional":             pos.Notional.StringFixed(2),
					"entryTime":            pos.EntryTime.Format(time.RFC3339),
					"holdDuration":         time.Since(pos.EntryTime).Round(time.Second).String(),
					"unrealizedPnL":        unrealizedPnL.StringFixed(4),
					"unrealizedPnLPercent": unrealizedPnLPercent.StringFixed(4),
					"leverage":             pos.Leverage,
					"status":               pos.Status,
				})
			}
		}
	case "normal":
		if s.tradingBot != nil {
			// Normal trading bot positions would go here
			// For now, return empty as the structure may differ
		}
	case "grid":
		if s.gridBot != nil {
			s.gridBot.Mu().RLock()
			for symbol, pos := range s.gridBot.Positions {
				if pos.Status == "OPEN" {
					currentPrice := s.getCurrentPrice(symbol, "LONG")
					entryPriceDec := udecimal.MustFromFloat64(pos.EntryPrice)
					quantityDec := udecimal.MustFromFloat64(pos.Quantity)
					unrealizedPnL := currentPrice.Sub(entryPriceDec).Mul(quantityDec)

					var unrealizedPnLPercent udecimal.Decimal
					if div, err := currentPrice.Sub(entryPriceDec).Div(entryPriceDec); err == nil {
						unrealizedPnLPercent = div.Mul(udecimal.MustFromFloat64(100))
					} else {
						unrealizedPnLPercent = udecimal.Zero
					}

					positions = append(positions, map[string]interface{}{
						"symbol":               symbol,
						"side":                 "LONG",
						"entryPrice":           fmt.Sprintf("%.8f", pos.EntryPrice),
						"currentPrice":         currentPrice.StringFixed(8),
						"quantity":             fmt.Sprintf("%.8f", pos.Quantity),
						"entryTime":            pos.EntryTime.Format(time.RFC3339),
						"holdDuration":         time.Since(pos.EntryTime).Round(time.Second).String(),
						"unrealizedPnL":        unrealizedPnL.StringFixed(4),
						"unrealizedPnLPercent": unrealizedPnLPercent.StringFixed(4),
						"targetProfit":         fmt.Sprintf("%.4f", pos.TargetProfit),
						"stopLoss":             fmt.Sprintf("%.4f", pos.StopLoss),
						"status":               pos.Status,
					})
				}
			}
			s.gridBot.Mu().RUnlock()
		}
	}

	sort.SliceStable(positions, func(i, j int) bool {
		symbolI, _ := positions[i]["symbol"].(string)
		symbolJ, _ := positions[j]["symbol"].(string)
		return symbolI < symbolJ
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(positions)
}

// handleStats returns trading statistics
func (s *Server) handleStats(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"tradingMode": s.cfg.GeneralConfig.TradingMode,
		"uptime":      time.Since(s.startTime).String(),
	}

	switch s.cfg.GeneralConfig.TradingMode {
	case "margin":
		if s.marginBot != nil {
			simStats := s.marginBot.GetSimStats()
			if simStats != nil {
				stats["totalTrades"] = simStats.TotalTrades
				stats["winningTrades"] = simStats.WinningTrades
				stats["losingTrades"] = simStats.LosingTrades
				stats["totalPnL"] = simStats.TotalPnL.StringFixed(4)
				stats["totalPnLPercent"] = simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)
				stats["bestTradePnL"] = simStats.BestTradePnL.StringFixed(4)
				stats["worstTradePnL"] = simStats.WorstTradePnL.StringFixed(4)
				stats["totalVolume"] = simStats.TotalVolume.StringFixed(2)
				stats["startTime"] = simStats.StartTime.Format(time.RFC3339)
				stats["lastTradeTime"] = simStats.LastTradeTime.Format(time.RFC3339)
			}
			positions := s.marginBot.Positions()
			stats["openPositions"] = len(positions)
		}
	case "futures":
		if s.futuresBot != nil {
			simStats := s.futuresBot.GetSimStats()
			if simStats != nil {
				stats["totalTrades"] = simStats.TotalTrades
				stats["winningTrades"] = simStats.WinningTrades
				stats["losingTrades"] = simStats.LosingTrades
				stats["totalPnL"] = simStats.TotalPnL.StringFixed(4)
				stats["totalPnLPercent"] = simStats.TotalPnLPercent.Mul(udecimal.MustFromFloat64(100)).StringFixed(4)
				stats["bestTradePnL"] = simStats.BestTradePnL.StringFixed(4)
				stats["worstTradePnL"] = simStats.WorstTradePnL.StringFixed(4)
				stats["totalVolume"] = simStats.TotalVolume.StringFixed(2)
				stats["startTime"] = simStats.StartTime.Format(time.RFC3339)
				stats["lastTradeTime"] = simStats.LastTradeTime.Format(time.RFC3339)
			}
			positions := s.futuresBot.Positions()
			stats["openPositions"] = len(positions)
		}
	case "grid":
		if s.gridBot != nil {
			s.gridBot.Mu().RLock()
			openCount := 0
			for _, pos := range s.gridBot.Positions {
				if pos.Status == "OPEN" {
					openCount++
				}
			}
			s.gridBot.Mu().RUnlock()
			stats["openPositions"] = openCount
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// handleConfig returns current configuration (sanitized)
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config := map[string]interface{}{
		"tradingMode":    s.cfg.GeneralConfig.TradingMode,
		"simulationMode": s.cfg.GeneralConfig.SimulationMode,
		"futures":        s.cfg.GeneralConfig.Futures,
		"feeRate":        s.cfg.GeneralConfig.FeeRate,
		"debugMode":      s.cfg.GeneralConfig.DebugMode,
	}

	// Add mode-specific config
	switch s.cfg.GeneralConfig.TradingMode {
	case "margin":
		config["marginConfig"] = map[string]interface{}{
			"positionSize":   s.cfg.MarginConfig.USDTPositionSize,
			"maxPositions":   s.cfg.MarginConfig.MaxPositions,
			"stopLoss":       s.cfg.MarginConfig.StopLoss,
			"trailingStart":  s.cfg.MarginConfig.TrailingStart,
			"trailingGap":    s.cfg.MarginConfig.TrailingGap,
			"entryChange":    s.cfg.MarginConfig.EntryChange,
			"lookbackPoints": s.cfg.MarginConfig.LookbackPoints,
			"checkInterval":  s.cfg.MarginConfig.CheckInterval,
			"maxHoldTime":    s.cfg.MarginConfig.MaxHoldTime,
			"baseAsset":      s.cfg.MarginConfig.BaseAsset,
			"quoteAssets":    s.cfg.MarginConfig.QuoteAssets,
			"marginType":     s.cfg.MarginConfig.MarginType,
		}
	case "futures":
		config["futuresConfig"] = map[string]interface{}{
			"positionSize":   s.cfg.FuturesConfig.USDTPositionSize,
			"maxPositions":   s.cfg.FuturesConfig.MaxPositions,
			"stopLoss":       s.cfg.FuturesConfig.StopLoss,
			"trailingStart":  s.cfg.FuturesConfig.TrailingStart,
			"trailingGap":    s.cfg.FuturesConfig.TrailingGap,
			"entryChange":    s.cfg.FuturesConfig.EntryChange,
			"lookbackPoints": s.cfg.FuturesConfig.LookbackPoints,
			"leverage":       s.cfg.FuturesConfig.Leverage,
			"checkInterval":  s.cfg.FuturesConfig.CheckInterval,
			"baseAsset":      s.cfg.FuturesConfig.BaseAsset,
			"quoteAssets":    s.cfg.FuturesConfig.QuoteAssets,
		}
	case "grid":
		config["gridConfig"] = map[string]interface{}{
			"minPriceChange": s.cfg.GridConfig.GridMinPriceChange,
			"profitTarget":   s.cfg.GridConfig.GridProfitTarget,
			"stopLoss":       s.cfg.GridConfig.GridStopLoss,
			"maxHoldTime":    s.cfg.GridConfig.GridMaxHoldTime,
			"maxPositions":   s.cfg.GridConfig.GridMaxPositions,
			"checkInterval":  s.cfg.GridConfig.GridCheckInterval,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(config)
}

// handleRecentTrades returns recent closed trades
func (s *Server) handleRecentTrades(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	trades := make([]map[string]interface{}, 0)

	switch s.cfg.GeneralConfig.TradingMode {
	case "margin":
		if s.marginBot != nil {
			closed := s.marginBot.ClosedPositions()
			for symbol, positions := range closed {
				for _, cp := range positions {
					trades = append(trades, map[string]interface{}{
						"symbol":       symbol,
						"side":         cp.EntryPosition.Side,
						"entryPrice":   cp.EntryPosition.EntryPrice.StringFixed(8),
						"exitPrice":    cp.ClosingPrice,
						"quantity":     cp.EntryPosition.Quantity.StringFixed(8),
						"entryTime":    cp.EntryPosition.EntryTime.Format(time.RFC3339),
						"exitTime":     cp.ClosingTime.Format(time.RFC3339),
						"pnl":          cp.Profit,
						"pnlPercent":   cp.PnLPercent,
						"reason":       cp.Reason,
						"holdDuration": cp.HoldDuration,
						"notional":     cp.EntryPosition.Notional.StringFixed(2),
					})
				}
			}
		}
	case "futures":
		if s.futuresBot != nil {
			closed := s.futuresBot.ClosedPositions()
			for symbol, positions := range closed {
				for _, cp := range positions {
					trades = append(trades, map[string]interface{}{
						"symbol":       symbol,
						"side":         cp.EntryPosition.Side,
						"entryPrice":   cp.EntryPosition.EntryPrice.StringFixed(8),
						"exitPrice":    cp.ClosingPrice,
						"quantity":     cp.EntryPosition.Quantity.StringFixed(8),
						"entryTime":    cp.EntryPosition.EntryTime.Format(time.RFC3339),
						"exitTime":     cp.ClosingTime.Format(time.RFC3339),
						"pnl":          cp.Profit,
						"pnlPercent":   cp.PnLPercent,
						"reason":       cp.Reason,
						"holdDuration": cp.HoldDuration,
						"notional":     cp.EntryPosition.Notional.StringFixed(2),
						"leverage":     cp.EntryPosition.Leverage,
					})
				}
			}
		}
	}

	// Sort by exit time (most recent first) and limit to 50
	sort.SliceStable(trades, func(i, j int) bool {
		exitTimeI, _ := time.Parse(time.RFC3339, trades[i]["exitTime"].(string))
		exitTimeJ, _ := time.Parse(time.RFC3339, trades[j]["exitTime"].(string))
		return exitTimeI.After(exitTimeJ)
	})

	if len(trades) > 50 {
		trades = trades[len(trades)-50:]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(trades)
}

// handleOrderBook returns order book statistics
func (s *Server) handleOrderBook(w http.ResponseWriter, r *http.Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stats := map[string]interface{}{
		"totalSymbols": s.orderBook.Symbols.Count(),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(stats)
}

// Helper functions

func (s *Server) getCurrentPrice(symbol string, side string) udecimal.Decimal {
	symbolObj, ok := s.orderBook.Symbols.Get(symbol)
	if !ok {
		return udecimal.Zero
	}
	bookTicker := symbolObj.GetBookTicker()
	if bookTicker == nil {
		return udecimal.Zero
	}
	// Use exit price for PnL calculations: Bid for LONG (sell at bid), Ask for SHORT (buy back at ask)
	if side == "LONG" {
		bidPrice, err := udecimal.Parse(bookTicker.BidPrice)
		if err != nil {
			return udecimal.Zero
		}
		return bidPrice
	} else { // SHORT
		askPrice, err := udecimal.Parse(bookTicker.AskPrice)
		if err != nil {
			return udecimal.Zero
		}
		return askPrice
	}
}

func (s *Server) calculateUnrealizedPnL(entryPrice, currentPrice, quantity udecimal.Decimal, side string) udecimal.Decimal {
	if side == "LONG" {
		return currentPrice.Sub(entryPrice).Mul(quantity)
	} else { // SHORT
		return entryPrice.Sub(currentPrice).Mul(quantity)
	}
}

func (s *Server) calculateUnrealizedPnLPercent(entryPrice, currentPrice udecimal.Decimal, side string) udecimal.Decimal {
	if entryPrice.IsZero() {
		return udecimal.Zero
	}
	if side == "LONG" {
		if div, err := currentPrice.Sub(entryPrice).Div(entryPrice); err == nil {
			return div.Mul(udecimal.MustFromFloat64(100))
		}
		return udecimal.Zero
	} else { // SHORT
		if div, err := entryPrice.Sub(currentPrice).Div(entryPrice); err == nil {
			return div.Mul(udecimal.MustFromFloat64(100))
		}
		return udecimal.Zero
	}
}

// handleClosePosition handles requests to close a position
func (s *Server) handleClosePosition(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Symbol string `json:"symbol"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Symbol == "" {
		http.Error(w, "Symbol is required", http.StatusBadRequest)
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var err error
	switch s.cfg.GeneralConfig.TradingMode {
	case "margin":
		if s.marginBot != nil {
			err = s.marginBot.ClosePosition(req.Symbol)
		} else {
			err = fmt.Errorf("margin bot not initialized")
		}
	case "futures":
		if s.futuresBot != nil {
			err = s.futuresBot.ClosePosition(req.Symbol)
		} else {
			err = fmt.Errorf("futures bot not initialized")
		}
	case "normal":
		// Normal trading bot doesn't have ClosePosition yet
		err = fmt.Errorf("close position not supported for normal trading mode")
	case "grid":
		// Grid bot doesn't have ClosePosition yet
		err = fmt.Errorf("close position not supported for grid trading mode")
	default:
		err = fmt.Errorf("unsupported trading mode: %s", s.cfg.GeneralConfig.TradingMode)
	}

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"message": fmt.Sprintf("Position %s closed successfully", req.Symbol),
	})
}
