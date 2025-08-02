package arbitrage

import (
	"arbitrage/currency"
	"arbitrage/utils"

	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog/log"
)

type Triangle struct {
	PathA Path
	PathB Path
	PathC Path
}

type Path struct {
	Pair      currency.Pair
	Ask       string
	Bid       string
	Direction string // Buy or sell
}

func (t *Triangle) CheckArbitrage(cfg *utils.Config) (found bool, profit float64, err error) {
	var priceA, priceB, priceC udecimal.Decimal

	fee, err := udecimal.NewFromFloat64(cfg.FeeRate)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing fee. Using default fee of 0.001 (1%%)")
		fee = udecimal.MustParse("0.001")
	}

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

	feeMultiplier := udecimal.MustParse("1").Sub(fee)

	var step1 udecimal.Decimal

	if t.PathA.Direction == "SELL" {
		step1, err = udecimal.MustParse("1").Div(priceA)
	} else {
		step1, err = priceA, nil
	}
	if err != nil {
		return false, 0, err
	}
	step1 = step1.Mul(feeMultiplier)

	step2 := step1.Mul(priceB).Mul(feeMultiplier)
	product := step2.Mul(priceC).Mul(feeMultiplier)

	if product.Cmp(udecimal.MustParse("1")) >= 1 {
		found = true
	}

	profit = product.InexactFloat64()

	return
}

func TestArbitrage() Triangle {
	triangle := Triangle{
		PathA: Path{Pair: currency.DOT_USDC, Direction: "SELL"},
		PathB: Path{Pair: currency.DOT_BTC, Direction: "BUY"},
		PathC: Path{Pair: currency.BTC_USDC, Direction: "SELL"},
	}

	return triangle
}
