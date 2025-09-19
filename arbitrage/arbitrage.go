package arbitrage

import (
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog/log"
)

type Triangle struct {
	PathA      *Path
	PathB      *Path
	PathC      *Path
	OnCooldown bool
	Locked     bool
	Lock       sync.Cond
	mu         sync.RWMutex

	// Price-based caching
	priceCache *PriceCache
}

func (t *Triangle) String() string {
	return fmt.Sprintf("[%s -> %s -> %s]", t.PathA.Pair.String(), t.PathB.Pair.String(), t.PathC.Pair.String())
}

type Path struct {
	Pair      currency.Pair
	bookData  atomic.Value // BookTicker
	Direction string       // Buy or sell
}

func (p *Path) SetBookTicker(book *BookTicker) {
	newBook := BookTicker{
		BidPrice: book.BidPrice,
		BidQty:   book.BidQty,
		AskPrice: book.AskPrice,
		AskQty:   book.AskQty,
		UpdateID: book.UpdateID,
	}
	p.bookData.Store(newBook)
}

func (p *Path) GetBookTicker() *BookTicker {
	if v := p.bookData.Load(); v != nil {
		book := v.(BookTicker)
		return &book
	}
	return nil
}

func (t *Triangle) updateTriangle(ob *Orderbook) error {
	// Check if another triangle is currently executing
	ob.ExecutionLock.Lock()
	if ob.IsExecuting {
		// Another triangle is executing, skip this one
		ob.ExecutionLock.Unlock()
		if ob.Config.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Triangle %s skipped - another triangle is executing", t.String())
		}
		return nil
	}
	ob.ExecutionLock.Unlock()

	err := t.TryExecuteTriangle(ob)
	if err != nil {
		return err
	}

	return nil
}

func (t *Triangle) TryExecuteTriangle(ob *Orderbook) error {
	if t.OnCooldown {
		return nil
	}

	found, profit, err := t.TestArbitrage(ob)
	if err != nil {
		return err
	}

	// logger.InfoFmt("Triangle %s simulated. Profit: %g USDC", t, profit)

	if found && profit > 0.01 {
		// Acquire global execution lock
		ob.ExecutionLock.Lock()
		if ob.IsExecuting {
			// Another triangle is already executing, skip this one
			ob.ExecutionLock.Unlock()
			logger.DebugFmt("Triangle %s skipped - another triangle is executing", t.String())
			return nil
		}

		// Mark that we're executing
		ob.IsExecuting = true
		ob.ExecutionLock.Unlock()

		// Use defer to ensure we always release the execution flag
		defer func() {
			ob.ExecutionLock.Lock()
			ob.IsExecuting = false
			ob.ExecutionLock.Unlock()
			logger.DebugFmt("Triangle %s execution completed, global lock released", t.String())
		}()

		logger.InfoFmt("%s %s - PROFIT: %g%%", logger.Green("Arbitrage found!"), t, profit)
		logger.DebugFmt("Triangle %s acquired global execution lock", t.String())
		if !ob.Config.GeneralConfig.SimulationMode {
			onCooldown, err := t.Execute(ob, ob.Config.TriangleConfig.OrderUSDCAmount)
			if err != nil {
				return err
			}
			if onCooldown {
				logger.WarningFmt("Triangle %s LOCKED from executing trades.", t.String())
				return fmt.Errorf("triangle %s on cooldown", t.String())
			}
		} else {
			log.Info().Str("triangle", t.String()).Msgf("%s", logger.Yellow("Would execute triangle if in LIVE mode."))
			ob.TradeLogger.Info().
				Str("triangle", t.String()).
				Float64("profit", profit).
				Msgf("Executed triangle %s for profit %g", t.String(), profit)

			// Lock the triangle to simulate LIVE mode behavior
			t.Cooldown(10)
		}
	}

	return nil
}

func (t *Triangle) CheckArbitrage(ob *Orderbook, fee float64) (found bool, profit float64, err error) {
	t.Lock.L.Lock()
	defer t.Lock.L.Unlock()
	if t.Locked {
		t.Lock.Wait()
	}

	var priceA, priceB, priceC udecimal.Decimal

	f, err := udecimal.NewFromFloat64(fee)
	if err != nil {
		logger.Warning("Error parsing fee. Using default fee of 0.001 (1%)")
		f = udecimal.MustParse("0.001")
	}

	// Get current price data from orderbook
	symbolA, ok := ob.Symbols.Get(t.PathA.Pair.String())
	if !ok {
		return false, 0, fmt.Errorf("symbol %s not found in orderbook", t.PathA.Pair.String())
	}
	bookA := symbolA.GetBookTicker()
	if bookA == nil {
		return false, 0, fmt.Errorf("no price data available for %s", t.PathA.Pair.String())
	}

	symbolB, ok := ob.Symbols.Get(t.PathB.Pair.String())
	if !ok {
		return false, 0, fmt.Errorf("symbol %s not found in orderbook", t.PathB.Pair.String())
	}
	bookB := symbolB.GetBookTicker()
	if bookB == nil {
		return false, 0, fmt.Errorf("no price data available for %s", t.PathB.Pair.String())
	}

	symbolC, ok := ob.Symbols.Get(t.PathC.Pair.String())
	if !ok {
		return false, 0, fmt.Errorf("symbol %s not found in orderbook", t.PathC.Pair.String())
	}
	bookC := symbolC.GetBookTicker()
	if bookC == nil {
		return false, 0, fmt.Errorf("no price data available for %s", t.PathC.Pair.String())
	}

	// Parse prices based on direction
	if t.PathA.Direction == "SELL" {
		priceA, err = udecimal.Parse(bookA.BidPrice)
	} else {
		priceA, err = udecimal.Parse(bookA.AskPrice)
	}
	if err != nil {
		return false, 0, err
	}

	if t.PathB.Direction == "SELL" {
		priceB, err = udecimal.Parse(bookB.BidPrice)
	} else {
		priceB, err = udecimal.Parse(bookB.AskPrice)
	}
	if err != nil {
		return false, 0, err
	}

	if t.PathC.Direction == "SELL" {
		priceC, err = udecimal.Parse(bookC.BidPrice)
	} else {
		priceC, err = udecimal.Parse(bookC.AskPrice)
	}
	if err != nil {
		return false, 0, err
	}

	feeMultiplier := udecimal.MustParse("1").Sub(f)

	// Step 1: Convert from starting currency
	var step1 udecimal.Decimal
	if t.PathA.Direction == "BUY" {
		step1, err = udecimal.MustParse("1").Div(priceA)
		if err != nil {
			return false, 0, err
		}
	} else {
		step1 = priceA
	}
	step1 = step1.Mul(feeMultiplier)

	// Step 2: Convert through second currency
	var step2 udecimal.Decimal
	if t.PathB.Direction == "BUY" {
		step2, err = step1.Div(priceB)
		if err != nil {
			return false, 0, err
		}
	} else {
		step2 = step1.Mul(priceB)
	}
	step2 = step2.Mul(feeMultiplier)

	// Step 3: Convert to final currency
	var product udecimal.Decimal
	if t.PathC.Direction == "BUY" {
		product, err = step2.Div(priceC)
		if err != nil {
			return false, 0, err
		}
	} else {
		product = step2.Mul(priceC)
	}
	product = product.Mul(feeMultiplier)

	if product.Cmp(udecimal.MustParse("1")) >= 1 {
		found = true
	}

	// profit = product.InexactFloat64() + 0.0121
	profit = product.InexactFloat64()

	return
}

func (t *Triangle) SimulateArbitrage(ob *Orderbook) (found bool, profit float64, err error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	initialAmount := ob.Config.TriangleConfig.SimulationUSDCAmount
	amount := initialAmount
	paths := [3]*Path{t.PathA, t.PathB, t.PathC}
	for _, p := range paths {
		s, ok := ob.Symbols.Get(p.Pair.String())
		if !ok {
			return false, 0, fmt.Errorf("symbol %s not found in orderbook", p.Pair.String())
		}

		// Get current price data from the orderbook instead of using cached data
		currentBookTicker := s.GetBookTicker()
		if currentBookTicker == nil {
			return false, 0, fmt.Errorf("no price data available for %s", p.Pair.String())
		}

		executedQty, quoteQty, err := p.simulatePathWithBookTicker(ob, s, amount, currentBookTicker)
		if ob.Config.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Simulated path %s %s with amount %g: executedQty %g - quoteQty %g (Ask: %s, Bid: %s)",
				p.Direction, p.Pair.String(), amount, executedQty, quoteQty,
				currentBookTicker.AskPrice, currentBookTicker.BidPrice)
		}
		if err != nil {
			return false, 0, err
		}

		if p.Direction == "BUY" {
			amount = executedQty
		} else {
			amount = quoteQty
		}

		// Debug: Log intermediate amounts
		if ob.Config.GeneralConfig.VerboseLogging {
			logger.DebugFmt("Triangle %s after path %s: amount=%g", t, p.Pair.String(), amount)
		}
	}

	if amount >= initialAmount {
		found = true
	}
	profit = (amount - initialAmount) * (ob.Config.TriangleConfig.OrderUSDCAmount / ob.Config.TriangleConfig.SimulationUSDCAmount)

	// Debug: Log the final calculation details
	if ob.Config.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Triangle %s FINAL CALC: initialAmount=%g, finalAmount=%g, profit=%g",
			t, initialAmount, amount, profit)
	}

	return
}

// TestArbitrage checks for arbitrage opportunities using price-based caching
func (t *Triangle) TestArbitrage(ob *Orderbook) (found bool, profit float64, err error) {
	// Initialize cache if not exists
	if t.priceCache == nil {
		t.priceCache = NewPriceCache()
	}

	// Variables for colorizing cache size output
	thousand := float64(1000)

	// Get current prices for all three paths
	paths := [3]*Path{t.PathA, t.PathB, t.PathC}
	var priceTuples [3]PriceTuple

	for i, p := range paths {
		s, ok := ob.Symbols.Get(p.Pair.String())
		if !ok {
			return false, 0, fmt.Errorf("symbol %s not found in orderbook", p.Pair.String())
		}

		currentBookTicker := s.GetBookTicker()
		if currentBookTicker == nil {
			return false, 0, fmt.Errorf("no price data available for %s", p.Pair.String())
		}

		priceTuples[i] = PriceTuple{
			Ask: currentBookTicker.AskPrice,
			Bid: currentBookTicker.BidPrice,
		}
	}

	// Create cache key
	tc := TriangleCache{
		SymbolA: priceTuples[0],
		SymbolB: priceTuples[1],
		SymbolC: priceTuples[2],
	}

	// Check cache first
	if result, exists := t.priceCache.Get(tc); exists {
		if ob.Config.GeneralConfig.VerboseLogging {
			logger.DebugFmt(
				"Triangle %s using %s result. Profit: %g USDC (Cache size: %s)",
				t,
				logger.Green("CACHED"),
				result.profit,
				logger.ColorizeNumber(float64(t.priceCache.Size()), nil, &thousand),
			)
		}
		return result.found, result.profit, result.err
	}

	// Debug: Log cache key details
	if ob.Config.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Triangle %s cache MISS. Key: A[%s/%s] B[%s/%s] C[%s/%s] (Cache size: %s)",
			t,
			tc.SymbolA.Ask, tc.SymbolA.Bid,
			tc.SymbolB.Ask, tc.SymbolB.Bid,
			tc.SymbolC.Ask, tc.SymbolC.Bid,
			logger.ColorizeNumber(float64(t.priceCache.Size()), nil, &thousand))
	}

	// Calculate new result
	found, profit, err = t.SimulateArbitrage(ob)

	// Cache the result
	t.priceCache.Set(tc, ArbitrageResult{
		found:  found,
		profit: profit,
		err:    err,
	})

	if ob.Config.GeneralConfig.VerboseLogging {
		logger.DebugFmt("Triangle %s %s new result. Profit: %g USDC (Cache size: %s)", t, logger.Blue("CALCULATED"), profit, logger.ColorizeNumber(float64(t.priceCache.Size()), nil, &thousand))
	}

	return found, profit, err
}

func (p *Path) simulatePath(ob *Orderbook, s *Symbol, amount float64) (executedQty, quoteQty float64, err error) {
	fee := ob.Config.GeneralConfig.FeeRate
	var quantity, price float64

	if p.Direction == "BUY" {
		bookTicker := s.GetBookTicker()
		if bookTicker == nil {
			err = fmt.Errorf("no book ticker data available for symbol %s", p.Pair.String())
			return
		}
		priceStr := bookTicker.AskPrice
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return
		}
		quantity = amount / price
	} else {
		bookTicker := s.GetBookTicker()
		if bookTicker == nil {
			err = fmt.Errorf("no book ticker data available for symbol %s", p.Pair.String())
			return
		}
		priceStr := bookTicker.BidPrice
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return
		}
		quantity = amount
	}

	if ob.Config.GeneralConfig.VerboseLogging {
		logger.DebugFmt("symbol: %s, price: %f, quantity: %f", s.Pair, price, quantity)
	}

	newQty, err := StepSizeQuantityFloat(s, quantity)
	if err != nil {
		return
	}

	executedQty = newQty
	quoteQty = executedQty * price * (1 - fee)

	return
}

func (p *Path) simulatePathWithBookTicker(ob *Orderbook, s *Symbol, amount float64, bookTicker *BookTicker) (executedQty, quoteQty float64, err error) {
	fee := ob.Config.GeneralConfig.FeeRate
	var quantity, price float64

	if p.Direction == "BUY" {
		priceStr := bookTicker.AskPrice
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return
		}
		quantity = amount / price
	} else {
		priceStr := bookTicker.BidPrice
		price, err = strconv.ParseFloat(priceStr, 64)
		if err != nil {
			return
		}
		quantity = amount
	}

	if ob.Config.GeneralConfig.VerboseLogging {
		logger.DebugFmt("symbol: %s, price: %f, quantity: %f", s.Pair, price, quantity)
	}

	newQty, err := StepSizeQuantityFloat(s, quantity)
	if err != nil {
		return
	}

	executedQty = newQty
	quoteQty = executedQty * price * (1 - fee)

	return
}

func (t *Triangle) Execute(ob *Orderbook, usdAmount float64) (onCooldown bool, err error) {
	if t.OnCooldown {
		return true, nil
	}

	// First trade (USDC -> first coin)
	s, found := ob.Symbols.Get(t.PathA.Pair.String())
	if !found {
		return false, fmt.Errorf("symbol %s not found in orderbook", t.PathA.Pair.String())
	}
	underMaxQty, err := checkMarketLotSize(s, t.PathA, usdAmount)
	if err != nil || !underMaxQty {
		// rollback order
		logger.WarningFmt("First path of triangle %s has quantity %g larger than MARKET_LOT_SIZE maxQuantity. Discarding triangle.", s.Pair, usdAmount)
		return false, err
	}

	res, err := t.PathA.Execute(ob.Client, s, usdAmount)
	if err != nil {
		return false, err
	}

	prevPath := t.PathA
	prevSymbol := s
	paths := [2]*Path{t.PathB, t.PathC}
	currentAmount := res.ExecutedQty
	for i, p := range paths {
		symbol, _ := ob.Symbols.Get(p.Pair.String())
		response, err := tryExecutePathWithRollback(ob.Client, prevSymbol, prevPath, symbol, p, currentAmount)
		if err != nil {
			return false, err
		}
		prevPath = p
		prevSymbol = symbol

		// For PathB to PathC transition, we need to check if the quote asset of PathB
		// matches the base asset of PathC - if it does, use CummulativeQuoteQty
		if i == 0 {
			// If PathB's quote asset matches PathC's base asset, use CummulativeQuoteQty
			if p.Pair.Quote == paths[1].Pair.Base {
				currentAmount = response.CummulativeQuoteQty
			} else {
				// If PathB's base asset matches PathC's base asset, use ExecutedQty
				currentAmount = response.ExecutedQty
			}
		}
	}

	logger.InfoFmt("%s", logger.Green("Triangle "+t.String()+" executed!"))

	// Lock the triangle to prevent rapid trade execution draining the balance
	t.Cooldown(10)

	balance, err := utils.CheckUSDCBalance(ob.Client)
	if err != nil {
		logger.Error(err)
	}
	logger.InfoFmt("%s", logger.Blue(fmt.Sprintf("The account has %s USDC", balance.Free)))

	return false, nil
}

func (t *Triangle) Cooldown(seconds int) {
	if t.OnCooldown {
		return
	}

	t.OnCooldown = true
	logger.InfoFmt("Triangle %s has been %s for %d seconds.", t.String(), logger.BgYellow("LOCKED"), seconds)
	go func() {
		time.Sleep(time.Second * time.Duration(seconds))
		t.OnCooldown = false
		logger.InfoFmt("Triangle %s has been %s.", t.String(), logger.BgGreen("UNLOCKED"))
	}()
}

func (p *Path) Execute(client *binance.Client, symbol *Symbol, amount float64) (res *binance.CreateOrderResponseFULL, err error) {
	var priceString string
	var quantity udecimal.Decimal
	var logAmount float64
	var baseQty, quoteQty float64

	if p.Direction == "BUY" {
		bookTicker := symbol.GetBookTicker()
		if bookTicker == nil {
			return nil, fmt.Errorf("no book ticker data available for symbol %s", p.Pair.String())
		}
		priceString = bookTicker.AskPrice
		price, err := udecimal.Parse(priceString)
		if err != nil {
			return nil, err
		}
		// For BUY orders: quantity = amount / price
		quantity, err = udecimal.MustFromFloat64(amount).Div(price)
		if err != nil {
			return nil, err
		}
		logAmount = amount
		quoteQty = logAmount
		baseQty = quantity.InexactFloat64()
	} else {
		bookTicker := symbol.GetBookTicker()
		if bookTicker == nil {
			return nil, fmt.Errorf("no book ticker data available for symbol %s", p.Pair.String())
		}
		priceString = bookTicker.BidPrice
		quantity = udecimal.MustFromFloat64(amount)
		logAmount = quantity.Mul(udecimal.MustParse(priceString)).InexactFloat64()
		quoteQty = logAmount
		baseQty = amount
	}

	log.Info().
		Str("symbol", symbol.Pair.String()).
		Float64("quoteQty", quoteQty).
		Float64("baseQty", baseQty).
		Str("price", priceString).
		Msgf("%sing %s %s for %g %s - price: %s", p.Direction, quantity, logger.Cyan(p.Pair.Base.String()), logAmount, logger.Cyan(p.Pair.Quote.String()), priceString)

	newQty, err := StepSizeQuantity(symbol, quantity)
	if err != nil {
		return nil, err
	}

	// if ok, err := checkQtyAndNotional(symbol, newQty, priceString); !ok {
	// 	return nil, err
	// }

	qty, err := strconv.ParseFloat(newQty.StringFixed(uint8(symbol.BasePrecision)), 64)
	if err != nil {
		return nil, err
	}

	res, err = NewMarketOrder(client, &symbol.Pair, p.Direction, qty)

	return
}

func checkQtyAndNotional(symbol *Symbol, quantity udecimal.Decimal, price string) (ok bool, err error) {
	// Check minimum quantity
	if symbol.Filter.LotSize.MinQty == "" {
		// If no LOT_SIZE filter is available, skip quantity check
		return true, nil
	}

	minQty, err := udecimal.Parse(symbol.Filter.LotSize.MinQty)
	if err != nil {
		return false, err
	}
	if quantity.Cmp(minQty) < 0 {
		return false, fmt.Errorf("quantity %s is smaller than minimum %s", quantity, minQty)
	}

	priceDecimal, err := udecimal.Parse(price)
	if err != nil {
		return false, err
	}

	// Check notional
	if symbol.Filter.MinNotional == "" {
		// If no NOTIONAL filter is available, skip notional check
		return true, nil
	}

	minNotional, err := udecimal.Parse(symbol.Filter.MinNotional)
	if err != nil {
		return false, err
	}
	notional := priceDecimal.Mul(quantity)
	if minNotional.Cmp(notional) > 0 {
		return false, fmt.Errorf("notional %s is smaller than minimum %s", notional, minNotional)
	}

	return true, nil
}

func StepSizeQuantity(symbol *Symbol, quantity udecimal.Decimal) (udecimal.Decimal, error) {
	if symbol.Filter.LotSize.StepSize == "" {
		// If no LOT_SIZE filter is available, return quantity as-is
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

func StepSizeQuantityFloat(symbol *Symbol, quantity float64) (float64, error) {
	if symbol.Filter.LotSize.StepSize == "" {
		// If no LOT_SIZE filter is available, return quantity as-is
		return quantity, nil
	}

	stepSize, err := strconv.ParseFloat(symbol.Filter.LotSize.StepSize, 64)
	if err != nil {
		return 0, err
	}

	lots := float64(int(quantity / stepSize))

	newQty := lots * stepSize

	return newQty, nil
}

func tryExecutePathWithRollback(client *binance.Client, prevSymbol *Symbol, prevPath *Path, symbol *Symbol, path *Path, amount string) (*binance.CreateOrderResponseFULL, error) {
	// Second trade (first coin -> second coin)
	prevAmount, err := strconv.ParseFloat(amount, 64)
	if err != nil {
		return nil, err
	}
	underMaxQty, err := checkMarketLotSize(symbol, path, prevAmount)
	if err != nil || !underMaxQty {
		// rollback order
		var realAmountStr string
		var realAmount udecimal.Decimal
		if path.Direction == "BUY" {
			amB := udecimal.MustFromFloat64(prevAmount)
			bookTicker := path.GetBookTicker()
			if bookTicker == nil {
				return nil, fmt.Errorf("no book ticker data available for symbol %s", path.Pair.String())
			}
			bid := udecimal.MustParse(bookTicker.BidPrice)
			realAmount, err = amB.Div(bid)
			if err != nil {
				return nil, err
			}
			realAmountStr = realAmount.String()
		}
		logger.WarningFmt("Path of triangle %s has quantity %s larger than MARKET_LOT_SIZE maxQuantity. Rolling back executed trades and discarding triangle.", symbol.Pair, realAmountStr)
		var dir string
		if prevPath.Direction == "BUY" {
			dir = "SELL"
		} else {
			dir = "BUY"
		}
		NewMarketOrder(client, &prevSymbol.Pair, dir, prevAmount)
		return nil, err
	}

	response, err := path.Execute(client, symbol, prevAmount)
	if err != nil {
		return nil, err
	}

	return response, nil
}

func checkMarketLotSize(s *Symbol, p *Path, qty float64) (bool, error) {
	// Check if MarketLotSize filter is available
	if s.Filter.MarketLotSize.MaxQty == "" {
		// If no MARKET_LOT_SIZE filter is available, skip this check
		return true, nil
	}

	marketMaxQty, err := udecimal.Parse(s.Filter.MarketLotSize.MaxQty)
	if err != nil {
		return false, err
	}

	quantity, err := udecimal.NewFromFloat64(qty)
	if err != nil {
		return false, err
	}

	if p.Direction == "BUY" {
		bookTicker := s.GetBookTicker()
		if bookTicker == nil {
			// If no book ticker data is available, skip this check
			return true, nil
		}

		priceString := bookTicker.AskPrice
		price, err := udecimal.Parse(priceString)
		if err != nil {
			return false, err
		}

		quantity, err = quantity.Div(price)
		if err != nil {
			return false, err
		}
	}

	if quantity.Cmp(marketMaxQty) >= 1 {
		return false, fmt.Errorf("quantity %s is larger than MARKET_LOT_SIZE maxQuantity %s", quantity, marketMaxQty)
	}

	return true, nil
}
