package arbitrage

import (
	"arbitrage/currency"
	"fmt"

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

	return fmt.Sprintf("%4s -> %-7s -> %-7s -> %4s", start, middle, end, start)
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

	profit = product.InexactFloat64()

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
