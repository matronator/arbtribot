package main

import (
	"arbitrage/arbitrage"
	"arbitrage/currency"
	"context"
	"strconv"

	"github.com/quagmt/udecimal"
)

func Simulate() {
	// BTC -> DOT -> BNB -> BTC
	// BTC -> XRP -> ETH -> BTC
	// BTC -> ADA -> BNB -> BTC
	// BTC -> LTC -> ETH -> BTC
	// BTC -> TRX -> BNB -> BTC

	t1 := arbitrage.Triangle{
		PathA: &arbitrage.Path{
			Pair:      currency.DOT_BTC,
			Direction: "BUY",
			Ask:       OrderBook.Symbols["DOTBTC"].BookTicker.AskPrice,
			Bid:       OrderBook.Symbols["DOTBTC"].BookTicker.BidPrice,
		},
		PathB: &arbitrage.Path{
			Pair:      currency.DOT_BNB,
			Direction: "SELL",
			Ask:       OrderBook.Symbols["DOTBNB"].BookTicker.AskPrice,
			Bid:       OrderBook.Symbols["DOTBNB"].BookTicker.BidPrice,
		},
		PathC: &arbitrage.Path{
			Pair:      currency.BNB_BTC,
			Direction: "SELL",
			Ask:       OrderBook.Symbols["BNBBTC"].BookTicker.AskPrice,
			Bid:       OrderBook.Symbols["BNBBTC"].BookTicker.BidPrice,
		},
	}

	found, profit, err := t1.CheckArbitrage(cfg.FeeRate)
	if err != nil {
		Error(err)
	}

	amt, _ := udecimal.MustParse(AccountBalances["BTC"].Free).Div(udecimal.MustParse("5"))
	qty, err := amt.Div(udecimal.MustParse(OrderBook.Symbols["DOTBTC"].BookTicker.AskPrice))
	if err != nil {
		Error(err)
	}
	InfoFmt("Buying %s DOT with %s BTC", qty, amt)

	quantity := qty.Trunc(8).InexactFloat64()

	res, err := client.NewExchangeInfoService().Symbol("DOTBTC").Do(context.Background())
	if err != nil {
		Error(err)
		return
	}

	var minQty, maxQty, stepSize string

	for _, symbol := range res.Symbols {
		if symbol.Symbol != "DOTBTC" {
			continue
		}

		for _, filter := range symbol.Filters {
			if filter.FilterType != "LOT_SIZE" {
				continue
			}

			minQty = filter.MinQty
			maxQty = filter.MaxQty
			stepSize = filter.StepSize
		}
	}

	stepSizeF, err := strconv.ParseFloat(stepSize, 32)
	if err != nil {
		Error(err)
		return
	}

	lots := quantity / stepSizeF
	lotsI := int(lots)

	newQty := float32(lotsI) * float32(stepSizeF)

	InfoFmt("New Quantity: %f | Min: %s Max: %s", newQty, minQty, maxQty)

	// newOrder, err := client.NewCreateOrderService().Symbol("BNBBTC").
	// 	Side("SELL").Type("MARKET").Quantity(0.023).
	// 	Do(context.Background())
	// if err != nil {
	// 	fmt.Println(err)
	// 	return
	// }
	// InfoFmt("%s", binance.PrettyPrint(newOrder))

	InfoFmt("Arbitrage found = %v | profit = %f", found, profit)
}
