package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"context"
	"time"

	"github.com/coder/websocket"
)

type SymbolVolume struct {
	Symbol string
	Volume float64
}

func FetchTopPairs(limit int) ([]string, error) {
	resp, err := http.Get("https://api.binance.com/api/v3/ticker/24hr")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := ioutil.ReadAll(resp.Body)
	var tickers []map[string]interface{}
	if err := json.Unmarshal(body, &tickers); err != nil {
		return nil, err
	}

	var volumes []SymbolVolume
	for _, t := range tickers {
		symbol, _ := t["symbol"].(string)
		volume, _ := t["quoteVolume"].(string)
		vol, _ := strconv.ParseFloat(volume, 64)
		volumes = append(volumes, SymbolVolume{Symbol: symbol, Volume: vol})
	}

	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Volume > volumes[j].Volume
	})

	var topPairs []string
	for i := 0; i < limit && i < len(volumes); i++ {
		topPairs = append(topPairs, volumes[i].Symbol)
	}

	return topPairs, nil
}

func StartWebSocketStreams(pairs []string, ob *OrderBookManager) error {
	for _, pair := range pairs {
		go func(symbol string) {
			fmt.Println("Starting WebSocket for", symbol)
			url := fmt.Sprintf("wss://stream.binance.com:9443/ws/%s@depth@100ms", strings.ToLower(symbol))

			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			conn, _, err := websocket.Dial(ctx, url, nil)
			if err != nil {
				fmt.Println("WebSocket connection error for", symbol, ":", err)
				return
			}
			defer conn.Close(websocket.StatusNormalClosure, "closing")

			buf := make([]byte, 8192)
			for {
				n, _, err := conn.Read(ctx)
				if err != nil {
					fmt.Println("WebSocket read error for", symbol, ":", err)
					break
				}
				ob.Update(symbol, buf[:n])
			}
		}(pair)
	}

	return nil
}
