package arbitrage

import (
	"arbitrage/currency"
	"context"
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
	res, err := t.PathA.Execute(client, *s, usdAmount)
	if err != nil {
		return err
	}

	// Second trade (first coin -> second coin)
	// Use ExecutedQty instead of CummulativeQuoteQty for more accuracy
	amountB, err := strconv.ParseFloat(res.ExecutedQty, 64)
	if err != nil {
		return err
	}
	resB, err := t.PathB.Execute(client, *ob.Symbols[t.PathB.Pair.String()], amountB)
	if err != nil {
		return err
	}

	// Final trade (second coin -> USDC)
	amountC, err := strconv.ParseFloat(resB.ExecutedQty, 64)
	if err != nil {
		return err
	}
	resC, err := t.PathC.Execute(client, *ob.Symbols[t.PathC.Pair.String()], amountC)
	if err != nil {
		return err
	}

	log.Info().Msgf("Triangle %s executed!", t.String())
	log.Info().Msg(binance.PrettyPrint(resC))

	return nil
}

func (p *Path) Execute(client *binance.Client, symbol Symbol, amount float64) (res *binance.CreateOrderResponseFULL, err error) {
	var priceString string
	var quantity udecimal.Decimal

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
	} else {
		priceString = p.Bid
		// For SELL orders: quantity = amount (we're selling the amount we have)
		quantity = udecimal.MustFromFloat64(amount)
	}

	stepSize, err := udecimal.Parse(symbol.Filter.LotSize.StepSize)
	if err != nil {
		return nil, err
	}

	minQty, err := udecimal.Parse(symbol.Filter.LotSize.MinQty)
	if err != nil {
		return nil, err
	}

	// Round quantity down to nearest step size
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

	// Check minimum notional value
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

	// Convert to float with proper precision
	qty, err := strconv.ParseFloat(newQty.StringFixed(uint8(symbol.BasePrecision)), 64)
	if err != nil {
		return nil, err
	}

	order, err := client.NewCreateOrderService().Symbol(symbol.Pair.String()).
		Quantity(qty).Type("MARKET").Side(p.Direction).Do(context.Background())
	if err != nil {
		return nil, err
	}
	res = order.(*binance.CreateOrderResponseFULL)

	log.Info().Msgf("Order for %s placed. %s %g %s for %s %s",
		symbol.Pair.String(), p.Direction, qty, symbol.Pair.Base, res.Price, symbol.Pair.Quote)
	log.Info().Msg(binance.PrettyPrint(res))

	return
}

func TestArbitrage() Triangle {
	triangle := Triangle{
		PathA: &Path{Pair: currency.DOT_USDC, Direction: "SELL"},
		PathB: &Path{Pair: currency.DOT_BTC, Direction: "BUY"},
		PathC: &Path{Pair: currency.BTC_USDC, Direction: "SELL"},
	}

	return triangle
}
