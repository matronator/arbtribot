package orderbook

import (
	"arbitrage/currency"
	"testing"
)

func TestUpdateBookTicker(t *testing.T) {
	ob := New()
	ob.Add(Symbol{
		Pair: currency.BNB_USDC,
	})
	ob.UpdateBookTicker("bnbusdc", &BookTicker{
		BidPrice: "12",
		AskPrice: "23",
		BidQty:   "1",
		AskQty:   "2",
		UpdateID: 1,
	})
	ob.UpdateBookTicker("btcdot", &BookTicker{})
}
