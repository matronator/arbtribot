package orderbook

import (
	"arbitrage/currency"
	"fmt"
	"time"
)

type Orderbook struct {
	Symbols map[string]Symbol
}

func New() *Orderbook {
	symbols := make(map[string]Symbol)
	return &Orderbook{Symbols: symbols}
}

func (ob *Orderbook) Add(s Symbol) {
	ob.Symbols[s.Pair.String()] = s
}

func (ob *Orderbook) UpdateBookTicker(key string, book *BookTicker) (updated bool, err error) {
	if val, ok := ob.Symbols[key]; ok {
		val.BookTicker = book
		ob.Symbols[key] = val
		return true, err
	}
	return false, fmt.Errorf("key %s doesn't exist on map Orderbook.Symbols", key)
}

type Symbol struct {
	Pair           currency.Pair
	BasePrecision  int8
	QuotePrecision int8
	Filter         ExchangeFilter
	BookTicker     *BookTicker
	LastUpdated    time.Time
}

type ExchangeFilter struct {
	MinPrice      string
	MaxPrice      string
	TickSize      string
	LotSize       LotSize
	MarketLotSize LotSize
	MinNotional   string
}

type LotSize struct {
	MinQty   string
	MaxQty   string
	StepSize string
}

type BookTicker struct {
	BidPrice string
	BidQty   string
	AskPrice string
	AskQty   string
	UpdateID int64
}

func (s *Symbol) UpdateSymbol(book BookTicker) {
	s.BookTicker = &book
}
