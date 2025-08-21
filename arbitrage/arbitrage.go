package arbitrage

import (
	"arbitrage/currency"
	"arbitrage/logger"
	"fmt"
	"strconv"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog/log"
)

type Triangle struct {
	PathA *Path
	PathB *Path
	PathC *Path
}

func (t *Triangle) String() string {
	var start, middle, end currency.Currency
	if t.PathA.Direction == "BUY" {
		start = t.PathA.Pair.Quote
	} else {
		start = t.PathA.Pair.Base
	}

	if t.PathB.Direction == "BUY" {
		middle = t.PathB.Pair.Quote
	} else {
		middle = t.PathB.Pair.Base
	}

	if t.PathC.Direction == "BUY" {
		end = t.PathC.Pair.Quote
	} else {
		end = t.PathC.Pair.Base
	}

	return fmt.Sprintf("[%s -> %s -> %s] %s -> %s -> %s -> %s", t.PathA.Pair.String(), t.PathB.Pair.String(), t.PathC.Pair.String(), start, middle, end, start)
}

type Path struct {
	Pair      currency.Pair
	Ask       string
	Bid       string
	Direction string // Buy or sell
}

func (t *Triangle) CheckArbitrage(fee float64) (found bool, profit float64, err error) {
	var priceA, priceB, priceC udecimal.Decimal

	f, err := udecimal.NewFromFloat64(fee)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing fee. Using default fee of 0.001 (1%)")
		f = udecimal.MustParse("0.001")
	}

	// Parse prices based on direction
	if t.PathA.Direction == "SELL" {
		priceA, err = udecimal.Parse(t.PathA.Bid)
	} else {
		priceA, err = udecimal.Parse(t.PathA.Ask)
	}
	if err != nil {
		return false, 0, err
	}

	if t.PathB.Direction == "SELL" {
		priceB, err = udecimal.Parse(t.PathB.Bid)
	} else {
		priceB, err = udecimal.Parse(t.PathB.Ask)
	}
	if err != nil {
		return false, 0, err
	}

	if t.PathC.Direction == "SELL" {
		priceC, err = udecimal.Parse(t.PathC.Bid)
	} else {
		priceC, err = udecimal.Parse(t.PathC.Ask)
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

	profit = product.InexactFloat64() + 0.0121

	return
}

func (t *Triangle) Execute(client *binance.Client, ob *Orderbook, usdAmount float64) error {
	// First trade (USDC -> first coin)
	s := ob.Symbols[t.PathA.Pair.String()]
	underMaxQty, err := checkMarketLotSize(s, t.PathA, usdAmount)
	if err != nil || !underMaxQty {
		// rollback order
		log.Warn().Msgf("First path of triangle %s has quantity %g larger than MARKET_LOT_SIZE maxQuantity. Discarding triangle.", s.Pair, usdAmount)
		return err
	}

	res, err := t.PathA.Execute(client, s, usdAmount)
	if err != nil {
		return err
	}

	prevPath := *t.PathA
	prevSymbol := *s
	paths := [2]*Path{t.PathB, t.PathC}
	currentAmount := res.ExecutedQty
	for i, p := range paths {
		symbol := ob.Symbols[p.Pair.String()]
		response, err := executePath(client, &prevSymbol, &prevPath, symbol, p, currentAmount)
		if err != nil {
			return err
		}
		prevPath = *p
		prevSymbol = *symbol

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

	log.Info().Msgf("%s", logger.Green("Triangle "+t.String()+" executed!"))

	return nil
}

func (p *Path) Execute(client *binance.Client, symbol *Symbol, amount float64) (res *binance.CreateOrderResponseFULL, err error) {
	var priceString string
	var quantity udecimal.Decimal
	var logAmount float64

	if p.Direction == "BUY" {
		priceString = p.Ask
		// For BUY orders: quantity = amount / price
		price, err := udecimal.Parse(priceString)
		if err != nil {
			return nil, err
		}
		quantity, err = udecimal.MustFromFloat64(amount).Div(price)
		if err != nil {
			return nil, err
		}
		logAmount = amount
	} else {
		priceString = p.Bid
		quantity = udecimal.MustFromFloat64(amount)
		logAmount = quantity.Mul(udecimal.MustParse(priceString)).InexactFloat64()
	}

	log.Info().Str("symbol", symbol.Pair.String()).Float64("amount", amount).
		Msgf("%sing %s %s for %g %s", p.Direction, quantity, logger.Cyan(p.Pair.Base.String()), logAmount, logger.Cyan(p.Pair.Quote.String()))

	stepSize, err := udecimal.Parse(symbol.Filter.LotSize.StepSize)
	if err != nil {
		return nil, err
	}

	minQty, err := udecimal.Parse(symbol.Filter.LotSize.MinQty)
	if err != nil {
		return nil, err
	}

	lots, err := quantity.Div(stepSize)
	if err != nil {
		return nil, err
	}
	lots = lots.Trunc(0)
	newQty := lots.Mul(stepSize)

	// Check minimum quantity
	if newQty.Cmp(minQty) < 0 {
		return nil, fmt.Errorf("quantity %s is smaller than minimum %s", newQty, minQty)
	}

	minNotional, err := udecimal.Parse(symbol.Filter.MinNotional)
	if err != nil {
		return nil, err
	}

	price, err := udecimal.Parse(priceString)
	if err != nil {
		return nil, err
	}

	notional := price.Mul(newQty)
	if minNotional.Cmp(notional) > 0 {
		return nil, fmt.Errorf("order notional %s is smaller than %s", notional, minNotional)
	}

	qty, err := strconv.ParseFloat(newQty.StringFixed(uint8(symbol.BasePrecision)), 64)
	if err != nil {
		return nil, err
	}

	res, err = NewMarketOrder(client, &symbol.Pair, p.Direction, qty)

	return
}

func executePath(client *binance.Client, prevSymbol *Symbol, prevPath *Path, symbol *Symbol, path *Path, amount string) (*binance.CreateOrderResponseFULL, error) {
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
			bid := udecimal.MustParse(path.Bid)
			realAmount, err = amB.Div(bid)
			if err != nil {
				return nil, err
			}
			realAmountStr = realAmount.String()
		}
		log.Warn().Msgf("Path of triangle %s has quantity %s larger than MARKET_LOT_SIZE maxQuantity. Rolling back executed trades and discarding triangle.", symbol.Pair, realAmountStr)
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
	marketMaxQty, err := udecimal.Parse(s.Filter.MarketLotSize.MaxQty)
	if err != nil {
		return false, err
	}

	quantity, err := udecimal.NewFromFloat64(qty)
	if err != nil {
		return false, err
	}

	if p.Direction == "BUY" {
		priceString := p.Ask
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

func TestArbitrage() Triangle {
	triangle := Triangle{
		PathA: &Path{Pair: currency.DOT_USDC, Direction: "SELL"},
		PathB: &Path{Pair: currency.DOT_BTC, Direction: "BUY"},
		PathC: &Path{Pair: currency.BTC_USDC, Direction: "SELL"},
	}

	return triangle
}
