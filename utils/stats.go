package utils

import (
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// RunStatistics holds all statistics for a trading run
type RunStatistics struct {
	RunID           string
	StartTime       time.Time
	EndTime         time.Time
	Config          *Config
	TradingMode     string
	TotalPnL        string
	TotalPnLPercent string
	TotalTrades     int
	WinningTrades   int
	LosingTrades    int
	Orders          []OrderRecord
}

// OrderRecord represents a single order/trade
type OrderRecord struct {
	Symbol       string
	Side         string
	EntryPrice   string
	ExitPrice    string
	Quantity     string
	EntryTime    time.Time
	ExitTime     time.Time
	PnL          string
	PnLPercent   string
	Reason       string
	HoldDuration string
	Notional     string
	MarginType   string
}

// SaveToCSV saves the statistics to a CSV file with a unique name
func (rs *RunStatistics) SaveToCSV() error {
	filename := fmt.Sprintf("stats_%s_%s.csv", rs.TradingMode, rs.RunID)
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("failed to create CSV file: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header section with config
	if err := rs.writeConfigSection(writer); err != nil {
		return err
	}

	// Write summary section
	if err := rs.writeSummarySection(writer); err != nil {
		return err
	}

	// Write orders section
	if err := rs.writeOrdersSection(writer); err != nil {
		return err
	}

	return nil
}

func (rs *RunStatistics) writeConfigSection(writer *csv.Writer) error {
	// Write section header
	if err := writer.Write([]string{"=== CONFIGURATION ==="}); err != nil {
		return err
	}
	writer.Write([]string{""})

	// General config
	writer.Write([]string{"GENERAL_CONFIG"})
	writer.Write([]string{"SIMULATION_MODE", strconv.FormatBool(rs.Config.GeneralConfig.SimulationMode)})
	writer.Write([]string{"DEBUG_MODE", strconv.FormatBool(rs.Config.GeneralConfig.DebugMode)})
	writer.Write([]string{"FEE_RATE", fmt.Sprintf("%.6f", rs.Config.GeneralConfig.FeeRate)})
	writer.Write([]string{"TRADING_MODE", rs.Config.GeneralConfig.TradingMode})
	writer.Write([]string{"FUTURES", strconv.FormatBool(rs.Config.GeneralConfig.Futures)})
	writer.Write([]string{""})

	// Trading mode specific config
	switch rs.TradingMode {
	case "margin":
		writer.Write([]string{"MARGIN_CONFIG"})
		writer.Write([]string{"USDT_POSITION_SIZE", fmt.Sprintf("%.2f", rs.Config.MarginConfig.USDTPositionSize)})
		writer.Write([]string{"MAX_POSITIONS", strconv.Itoa(rs.Config.MarginConfig.MaxPositions)})
		writer.Write([]string{"STOP_LOSS", fmt.Sprintf("%.6f", rs.Config.MarginConfig.StopLoss)})
		writer.Write([]string{"TRAILING_START", fmt.Sprintf("%.6f", rs.Config.MarginConfig.TrailingStart)})
		writer.Write([]string{"TRAILING_GAP", fmt.Sprintf("%.6f", rs.Config.MarginConfig.TrailingGap)})
		writer.Write([]string{"ENTRY_CHANGE", fmt.Sprintf("%.6f", rs.Config.MarginConfig.EntryChange)})
		writer.Write([]string{"LOOKBACK_POINTS", strconv.Itoa(rs.Config.MarginConfig.LookbackPoints)})
		writer.Write([]string{"CHECK_INTERVAL", strconv.Itoa(rs.Config.MarginConfig.CheckInterval)})
		writer.Write([]string{"BASE_ASSET", rs.Config.MarginConfig.BaseAsset})
		writer.Write([]string{"QUOTE_ASSETS", strings.Join(rs.Config.MarginConfig.QuoteAssets, ",")})
		writer.Write([]string{"MARGIN_TYPE", rs.Config.MarginConfig.MarginType})
	case "futures":
		writer.Write([]string{"FUTURES_CONFIG"})
		writer.Write([]string{"USDT_POSITION_SIZE", fmt.Sprintf("%.2f", rs.Config.FuturesConfig.USDTPositionSize)})
		writer.Write([]string{"MAX_POSITIONS", strconv.Itoa(rs.Config.FuturesConfig.MaxPositions)})
		writer.Write([]string{"STOP_LOSS", fmt.Sprintf("%.6f", rs.Config.FuturesConfig.StopLoss)})
		writer.Write([]string{"TRAILING_START", fmt.Sprintf("%.6f", rs.Config.FuturesConfig.TrailingStart)})
		writer.Write([]string{"TRAILING_GAP", fmt.Sprintf("%.6f", rs.Config.FuturesConfig.TrailingGap)})
		writer.Write([]string{"ENTRY_CHANGE", fmt.Sprintf("%.6f", rs.Config.FuturesConfig.EntryChange)})
		writer.Write([]string{"LOOKBACK_POINTS", strconv.Itoa(rs.Config.FuturesConfig.LookbackPoints)})
		writer.Write([]string{"LEVERAGE", strconv.Itoa(rs.Config.FuturesConfig.Leverage)})
		writer.Write([]string{"CHECK_INTERVAL", strconv.Itoa(rs.Config.FuturesConfig.CheckInterval)})
		writer.Write([]string{"BASE_ASSET", rs.Config.FuturesConfig.BaseAsset})
		writer.Write([]string{"QUOTE_ASSETS", strings.Join(rs.Config.FuturesConfig.QuoteAssets, ",")})
	case "normal":
		writer.Write([]string{"NORMAL_CONFIG"})
		writer.Write([]string{"USDC_AMOUNT", fmt.Sprintf("%.2f", rs.Config.NormalConfig.USDCAmount)})
		writer.Write([]string{"MAX_POSITIONS", strconv.Itoa(rs.Config.NormalConfig.MaxPositions)})
		writer.Write([]string{"STOP_LOSS", fmt.Sprintf("%.6f", rs.Config.NormalConfig.StopLoss)})
		writer.Write([]string{"MIN_PROFIT", fmt.Sprintf("%.6f", rs.Config.NormalConfig.MinProfit)})
		writer.Write([]string{"MAX_HOLD_TIME", strconv.Itoa(rs.Config.NormalConfig.MaxHoldTime)})
		writer.Write([]string{"TARGET_PROFIT", fmt.Sprintf("%.6f", rs.Config.NormalConfig.TargetProfit)})
		writer.Write([]string{"BASE_ASSET", rs.Config.NormalConfig.BaseAsset})
		writer.Write([]string{"QUOTE_ASSETS", strings.Join(rs.Config.NormalConfig.QuoteAssets, ",")})
	case "grid":
		writer.Write([]string{"GRID_CONFIG"})
		writer.Write([]string{"MIN_PRICE_CHANGE", fmt.Sprintf("%.6f", rs.Config.GridConfig.GridMinPriceChange)})
		writer.Write([]string{"PROFIT_TARGET", fmt.Sprintf("%.6f", rs.Config.GridConfig.GridProfitTarget)})
		writer.Write([]string{"STOP_LOSS", fmt.Sprintf("%.6f", rs.Config.GridConfig.GridStopLoss)})
		writer.Write([]string{"MAX_HOLD_TIME", strconv.Itoa(rs.Config.GridConfig.GridMaxHoldTime)})
		writer.Write([]string{"MAX_POSITIONS", strconv.Itoa(rs.Config.GridConfig.GridMaxPositions)})
		writer.Write([]string{"CHECK_INTERVAL", strconv.Itoa(rs.Config.GridConfig.GridCheckInterval)})
	case "triangle":
		writer.Write([]string{"TRIANGLE_CONFIG"})
		writer.Write([]string{"ORDER_USDC_AMOUNT", fmt.Sprintf("%.2f", rs.Config.TriangleConfig.OrderUSDCAmount)})
		writer.Write([]string{"SIMULATION_USDC_AMOUNT", fmt.Sprintf("%.2f", rs.Config.TriangleConfig.SimulationUSDCAmount)})
		writer.Write([]string{"START_ASSET", rs.Config.TriangleConfig.StartAsset})
		writer.Write([]string{"BASE_ASSETS", strings.Join(rs.Config.TriangleConfig.BaseAssets, ",")})
	}

	writer.Write([]string{""})
	return nil
}

func (rs *RunStatistics) writeSummarySection(writer *csv.Writer) error {
	writer.Write([]string{"=== SUMMARY ==="})
	writer.Write([]string{"RUN_ID", rs.RunID})
	writer.Write([]string{"TRADING_MODE", rs.TradingMode})
	writer.Write([]string{"START_TIME", rs.StartTime.Format(time.RFC3339)})
	writer.Write([]string{"END_TIME", rs.EndTime.Format(time.RFC3339)})
	writer.Write([]string{"DURATION", rs.EndTime.Sub(rs.StartTime).Round(time.Second).String()})
	writer.Write([]string{"TOTAL_TRADES", strconv.Itoa(rs.TotalTrades)})
	writer.Write([]string{"WINNING_TRADES", strconv.Itoa(rs.WinningTrades)})
	writer.Write([]string{"LOSING_TRADES", strconv.Itoa(rs.LosingTrades)})
	writer.Write([]string{"TOTAL_PNL", rs.TotalPnL})
	writer.Write([]string{"TOTAL_PNL_PERCENT", rs.TotalPnLPercent})
	writer.Write([]string{""})
	return nil
}

func (rs *RunStatistics) writeOrdersSection(writer *csv.Writer) error {
	writer.Write([]string{"=== ORDERS ==="})
	if len(rs.Orders) == 0 {
		writer.Write([]string{"No orders recorded"})
		writer.Write([]string{""})
		return nil
	}

	// Write header
	writer.Write([]string{
		"Symbol", "Side", "EntryPrice", "ExitPrice", "Quantity",
		"EntryTime", "ExitTime", "PnL", "PnLPercent", "Reason",
		"HoldDuration", "Notional", "MarginType",
	})

	// Write orders
	for _, order := range rs.Orders {
		writer.Write([]string{
			order.Symbol,
			order.Side,
			order.EntryPrice,
			order.ExitPrice,
			order.Quantity,
			order.EntryTime.Format(time.RFC3339),
			order.ExitTime.Format(time.RFC3339),
			order.PnL,
			order.PnLPercent,
			order.Reason,
			order.HoldDuration,
			order.Notional,
			order.MarginType,
		})
	}

	writer.Write([]string{""})
	return nil
}
