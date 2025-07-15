package main

import (
	"encoding/json"
	"sync"
	"strconv"
)

type OrderBook struct {
	BestAsk float64
	BestBid float64
}

type OrderBookManager struct {
	books map[string]*OrderBook
	mu    sync.RWMutex
}

func NewOrderBookManager() *OrderBookManager {
	return &OrderBookManager{
		books: make(map[string]*OrderBook),
	}
}

func (ob *OrderBookManager) Update(symbol string, message []byte) {
	var data map[string]interface{}
	if err := json.Unmarshal(message, &data); err != nil {
		return
	}

	asks, ok1 := data["asks"].([]interface{})
	bids, ok2 := data["bids"].([]interface{})
	if !ok1 || !ok2 || len(asks) == 0 || len(bids) == 0 {
		return
	}

	bestAsk := parsePrice(asks[0])
	bestBid := parsePrice(bids[0])

	ob.mu.Lock()
	defer ob.mu.Unlock()
	ob.books[symbol] = &OrderBook{
		BestAsk: bestAsk,
		BestBid: bestBid,
	}
}

func (ob *OrderBookManager) Get(symbol string) (OrderBook, bool) {
	ob.mu.RLock()
	defer ob.mu.RUnlock()
	book, ok := ob.books[symbol]
	if !ok {
		return OrderBook{}, false
	}
	return *book, true
}

func parsePrice(item interface{}) float64 {
	entry, ok := item.([]interface{})
	if !ok || len(entry) < 2 {
		return 0
	}
	priceStr, ok1 := entry[0].(string)
	price, err := strconv.ParseFloat(priceStr, 64)
	if !ok1 || err != nil {
		return 0
	}
	return price
}
