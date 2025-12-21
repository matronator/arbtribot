package trading

import (
	"arbtribot/currency"
	"arbtribot/logger"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/quagmt/udecimal"
)

const positionsFileName = "positions.json"

// PositionData represents all positions that can be persisted
type PositionData struct {
	NormalPositions  map[string]*NormalPositionData  `json:"normal_positions,omitempty"`
	MarginPositions  map[string]*MarginPositionData  `json:"margin_positions,omitempty"`
	FuturesPositions map[string]*FuturesPositionData `json:"futures_positions,omitempty"`
	GridPositions    map[string]*GridPositionData    `json:"grid_positions,omitempty"`
	SavedAt          time.Time                       `json:"saved_at"`
}

// NormalPositionData is a serializable version of Position
type NormalPositionData struct {
	Symbol           string        `json:"symbol"`
	BaseSymbol       string        `json:"base_symbol"`
	QuoteSymbol      string        `json:"quote_symbol"`
	EntryPrice       string        `json:"entry_price"`
	Quantity         string        `json:"quantity"`
	EntryTime        time.Time     `json:"entry_time"`
	ProfitPercentage float64       `json:"profit_percentage"`
	ProfitAmount     float64       `json:"profit_amount"`
	Status           string        `json:"status"`
	MaxHoldTime      time.Duration `json:"max_hold_time"`
}

// MarginPositionData is a serializable version of MarginPosition
type MarginPositionData struct {
	Symbol      string    `json:"symbol"`
	BaseSymbol  string    `json:"base_symbol"`
	QuoteSymbol string    `json:"quote_symbol"`
	Side        string    `json:"side"`
	EntryPrice  string    `json:"entry_price"`
	Quantity    string    `json:"quantity"`
	EntryTime   time.Time `json:"entry_time"`
	Status      string    `json:"status"`
	PeakPrice   string    `json:"peak_price"`
	TroughPrice string    `json:"trough_price"`
	Notional    string    `json:"notional"`
	BorrowedQty string    `json:"borrowed_qty"`
}

// FuturesPositionData is a serializable version of FuturesPosition
type FuturesPositionData struct {
	Symbol      string    `json:"symbol"`
	BaseSymbol  string    `json:"base_symbol"`
	QuoteSymbol string    `json:"quote_symbol"`
	Side        string    `json:"side"`
	EntryPrice  string    `json:"entry_price"`
	Quantity    string    `json:"quantity"`
	EntryTime   time.Time `json:"entry_time"`
	Status      string    `json:"status"`
	PeakPrice   string    `json:"peak_price"`
	TroughPrice string    `json:"trough_price"`
	Notional    string    `json:"notional"`
	Leverage    int       `json:"leverage"`
}

// GridPositionData is a serializable version of grid position
// Note: We use interface{} to avoid import cycle with grid package
type GridPositionData struct {
	Symbol       string        `json:"symbol"`
	BaseAsset    string        `json:"base_asset"`
	QuoteAsset   string        `json:"quote_asset"`
	EntryPrice   float64       `json:"entry_price"`
	Quantity     float64       `json:"quantity"`
	EntryTime    time.Time     `json:"entry_time"`
	TargetProfit float64       `json:"target_profit"`
	StopLoss     float64       `json:"stop_loss"`
	Status       string        `json:"status"`
	MaxHoldTime  time.Duration `json:"max_hold_time"`
}

// SavePositions saves all open positions to a JSON file
func SavePositions(tradingBot *TradingBot, marginBot *MarginBot, futuresBot *FuturesBot, gridPositions map[string]any, loaded bool, silent bool) error {
	data := &PositionData{
		SavedAt: time.Now(),
	}

	// Save normal trading positions
	if tradingBot != nil {
		tradingBot.Mu().Lock()
		normalPositions := make(map[string]*NormalPositionData)
		for symbol, pos := range tradingBot.Positions {
			if pos.Status == "OPEN" {
				normalPositions[symbol] = &NormalPositionData{
					Symbol:           symbol,
					BaseSymbol:       pos.Symbol.Base.Symbol,
					QuoteSymbol:      pos.Symbol.Quote.Symbol,
					EntryPrice:       pos.EntryPrice,
					Quantity:         pos.Quantity,
					EntryTime:        pos.EntryTime,
					ProfitPercentage: pos.ProfitPercentage,
					ProfitAmount:     pos.ProfitAmount,
					Status:           pos.Status,
					MaxHoldTime:      pos.MaxHoldTime,
				}
			}
		}
		tradingBot.Mu().Unlock()
		if len(normalPositions) > 0 {
			data.NormalPositions = normalPositions
		}
	}

	// Save margin positions
	if marginBot != nil {
		marginBot.Mu().Lock()
		marginPositions := make(map[string]*MarginPositionData)
		for symbol, pos := range marginBot.Positions() {
			if pos.Status == "OPEN" {
				marginPositions[symbol] = &MarginPositionData{
					Symbol:      symbol,
					BaseSymbol:  pos.Symbol.Base.Symbol,
					QuoteSymbol: pos.Symbol.Quote.Symbol,
					Side:        pos.Side,
					EntryPrice:  pos.EntryPrice.String(),
					Quantity:    pos.Quantity.String(),
					EntryTime:   pos.EntryTime,
					Status:      pos.Status,
					PeakPrice:   pos.PeakPrice.String(),
					TroughPrice: pos.TroughPrice.String(),
					Notional:    pos.Notional.String(),
					BorrowedQty: pos.BorrowedQty.String(),
				}
			}
		}
		marginBot.Mu().Unlock()
		if len(marginPositions) > 0 {
			data.MarginPositions = marginPositions
		}
	}

	// Save futures positions
	if futuresBot != nil {
		futuresBot.Mu().Lock()
		futuresPositions := make(map[string]*FuturesPositionData)
		for symbol, pos := range futuresBot.Positions() {
			if pos.Status == "OPEN" {
				futuresPositions[symbol] = &FuturesPositionData{
					Symbol:      symbol,
					BaseSymbol:  pos.Symbol.Base.Symbol,
					QuoteSymbol: pos.Symbol.Quote.Symbol,
					Side:        pos.Side,
					EntryPrice:  pos.EntryPrice.String(),
					Quantity:    pos.Quantity.String(),
					EntryTime:   pos.EntryTime,
					Status:      pos.Status,
					PeakPrice:   pos.PeakPrice.String(),
					TroughPrice: pos.TroughPrice.String(),
					Notional:    pos.Notional.String(),
					Leverage:    pos.Leverage,
				}
			}
		}
		futuresBot.Mu().Unlock()
		if len(futuresPositions) > 0 {
			data.FuturesPositions = futuresPositions
		}
	}

	// Save grid positions (passed as interface to avoid import cycle)
	if len(gridPositions) > 0 {
		gridPosData := make(map[string]*GridPositionData)
		for symbol, posInterface := range gridPositions {
			// Use type assertion - caller should pass map[string]*GridPositionData
			if posData, ok := posInterface.(*GridPositionData); ok && posData.Status == "OPEN" {
				gridPosData[symbol] = posData
			}
		}
		if len(gridPosData) > 0 {
			data.GridPositions = gridPosData
		}
	}

	// Check if there are any positions to save
	hasPositions := len(data.NormalPositions) > 0 ||
		len(data.MarginPositions) > 0 ||
		len(data.FuturesPositions) > 0 ||
		len(data.GridPositions) > 0

	if !hasPositions {
		// Before deleting, verify the file doesn't contain positions we might have missed
		// This prevents data loss if positions failed to load
		fileHasPositions, err := fileHasOpenPositions(positionsFileName)
		if err != nil {
			// If we can't read the file, don't delete it (safer)
			logger.WarningFmt("Could not verify positions file before deletion: %v", err)
			return nil
		}

		if !loaded && fileHasPositions {
			// File has positions but we don't have them in memory - don't delete!
			// This indicates positions failed to load, preserve the file
			logger.WarningFmt("Positions file contains open positions but none were loaded. Preserving file to prevent data loss.")
		}
		return nil
	}

	// Write to file
	file, err := os.Create(positionsFileName)
	if err != nil {
		return fmt.Errorf("failed to create positions file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode positions: %w", err)
	}

	if !silent {
		logger.InfoFmt("Saved %d open positions to %s",
			len(data.NormalPositions)+len(data.MarginPositions)+len(data.FuturesPositions)+len(data.GridPositions),
			positionsFileName)
	}

	return nil
}

// LoadPositions loads positions from a JSON file and restores them to the bots
// gridPositions should be a map that will be populated with loaded grid positions
func LoadPositions(tradingBot *TradingBot, marginBot *MarginBot, futuresBot *FuturesBot, gridPositions map[string]interface{}) error {
	file, err := os.Open(positionsFileName)
	if err != nil {
		if os.IsNotExist(err) {
			// No saved positions file, that's fine
			return nil
		}
		return fmt.Errorf("failed to open positions file: %w", err)
	}
	defer file.Close()

	var data PositionData
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return fmt.Errorf("failed to decode positions file: %w", err)
	}

	loadedCount := 0

	// Load normal trading positions
	if tradingBot != nil && len(data.NormalPositions) > 0 {
		tradingBot.Mu().Lock()
		for symbol, posData := range data.NormalPositions {
			if posData.Status == "OPEN" {
				pair := currency.Pair{
					Base:  currency.Currency{Symbol: posData.BaseSymbol},
					Quote: currency.Currency{Symbol: posData.QuoteSymbol},
				}
				pos := &Position{
					Symbol:           pair,
					EntryPrice:       posData.EntryPrice,
					Quantity:         posData.Quantity,
					EntryTime:        posData.EntryTime,
					ProfitPercentage: posData.ProfitPercentage,
					ProfitAmount:     posData.ProfitAmount,
					Status:           posData.Status,
					MaxHoldTime:      posData.MaxHoldTime,
				}
				tradingBot.Positions[symbol] = pos
				loadedCount++
			}
		}
		tradingBot.Mu().Unlock()
	}

	// Load margin positions
	if marginBot != nil && len(data.MarginPositions) > 0 {
		marginBot.Mu().Lock()
		for symbol, posData := range data.MarginPositions {
			if posData.Status == "OPEN" {
				pair := currency.Pair{
					Base:  currency.Currency{Symbol: posData.BaseSymbol},
					Quote: currency.Currency{Symbol: posData.QuoteSymbol},
				}
				entryPriceFloat, _ := strconv.ParseFloat(posData.EntryPrice, 64)
				quantityFloat, _ := strconv.ParseFloat(posData.Quantity, 64)
				peakPriceFloat, _ := strconv.ParseFloat(posData.PeakPrice, 64)
				troughPriceFloat, _ := strconv.ParseFloat(posData.TroughPrice, 64)
				notionalFloat, _ := strconv.ParseFloat(posData.Notional, 64)
				borrowedQtyFloat, _ := strconv.ParseFloat(posData.BorrowedQty, 64)

				entryPrice := udecimal.MustFromFloat64(entryPriceFloat)
				quantity := udecimal.MustFromFloat64(quantityFloat)
				peakPrice := udecimal.MustFromFloat64(peakPriceFloat)
				troughPrice := udecimal.MustFromFloat64(troughPriceFloat)
				notional := udecimal.MustFromFloat64(notionalFloat)
				borrowedQty := udecimal.MustFromFloat64(borrowedQtyFloat)

				pos := &MarginPosition{
					Symbol:      pair,
					Side:        posData.Side,
					EntryPrice:  entryPrice,
					Quantity:    quantity,
					EntryTime:   posData.EntryTime,
					Status:      posData.Status,
					PeakPrice:   peakPrice,
					TroughPrice: troughPrice,
					Notional:    notional,
					BorrowedQty: borrowedQty,
				}
				marginBot.Positions()[symbol] = pos
				loadedCount++
			}
		}
		marginBot.Mu().Unlock()
	}

	// Load futures positions
	if futuresBot != nil && len(data.FuturesPositions) > 0 {
		futuresBot.Mu().Lock()
		for symbol, posData := range data.FuturesPositions {
			if posData.Status == "OPEN" {
				pair := currency.Pair{
					Base:  currency.Currency{Symbol: posData.BaseSymbol},
					Quote: currency.Currency{Symbol: posData.QuoteSymbol},
				}
				entryPriceFloat, _ := strconv.ParseFloat(posData.EntryPrice, 64)
				quantityFloat, _ := strconv.ParseFloat(posData.Quantity, 64)
				peakPriceFloat, _ := strconv.ParseFloat(posData.PeakPrice, 64)
				troughPriceFloat, _ := strconv.ParseFloat(posData.TroughPrice, 64)
				notionalFloat, _ := strconv.ParseFloat(posData.Notional, 64)

				entryPrice := udecimal.MustFromFloat64(entryPriceFloat)
				quantity := udecimal.MustFromFloat64(quantityFloat)
				peakPrice := udecimal.MustFromFloat64(peakPriceFloat)
				troughPrice := udecimal.MustFromFloat64(troughPriceFloat)
				notional := udecimal.MustFromFloat64(notionalFloat)

				pos := &FuturesPosition{
					Symbol:      pair,
					Side:        posData.Side,
					EntryPrice:  entryPrice,
					Quantity:    quantity,
					EntryTime:   posData.EntryTime,
					Status:      posData.Status,
					PeakPrice:   peakPrice,
					TroughPrice: troughPrice,
					Notional:    notional,
					Leverage:    posData.Leverage,
				}
				futuresBot.Positions()[symbol] = pos
				loadedCount++
			}
		}
		futuresBot.Mu().Unlock()
	}

	// Load grid positions (caller should handle the actual grid bot restoration)
	if gridPositions != nil && len(data.GridPositions) > 0 {
		for symbol, posData := range data.GridPositions {
			if posData.Status == "OPEN" {
				gridPositions[symbol] = posData
				loadedCount++
			}
		}
	}

	if loadedCount > 0 {
		logger.InfoFmt("Loaded %d open positions from %s (saved at %s)",
			loadedCount, positionsFileName, data.SavedAt.Format(time.RFC3339))

		os.Remove(positionsFileName)
		logger.InfoFmt("Removed empty positions file")
	}

	return nil
}

// fileHasOpenPositions checks if the positions file contains any open positions
func fileHasOpenPositions(filename string) (bool, error) {
	file, err := os.Open(filename)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	defer file.Close()

	var data PositionData
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&data); err != nil {
		return false, err
	}

	// Check if any position type has open positions
	hasOpen := false
	if len(data.NormalPositions) > 0 {
		for _, pos := range data.NormalPositions {
			if pos.Status == "OPEN" {
				hasOpen = true
				break
			}
		}
	}
	if !hasOpen && len(data.MarginPositions) > 0 {
		for _, pos := range data.MarginPositions {
			if pos.Status == "OPEN" {
				hasOpen = true
				break
			}
		}
	}
	if !hasOpen && len(data.FuturesPositions) > 0 {
		for _, pos := range data.FuturesPositions {
			if pos.Status == "OPEN" {
				hasOpen = true
				break
			}
		}
	}
	if !hasOpen && len(data.GridPositions) > 0 {
		for _, pos := range data.GridPositions {
			if pos.Status == "OPEN" {
				hasOpen = true
				break
			}
		}
	}

	return hasOpen, nil
}

// GetPositionsFilePath returns the full path to the positions file
func GetPositionsFilePath() string {
	absPath, err := filepath.Abs(positionsFileName)
	if err != nil {
		return positionsFileName
	}
	return absPath
}
