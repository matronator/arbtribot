package main

import (
	"arbtribot/arbitrage"
	"os"
	"os/signal"
	"syscall"

	binance "github.com/binance/binance-connector-go"
)

func Test() {
	client := binance.NewWebsocketAPIClient(cfg.APIKey, cfg.APISecret)

	err := client.Connect()
	if err != nil {
		Error(err)
		return
	}
	defer client.Close()
}

func ConnectToExchange(symbols []string, ob *arbitrage.Orderbook) {
	websocketStreamClient := binance.NewWebsocketStreamClient(true)

	wsBookTickerHandler := func(event *binance.WsBookTickerEvent) {
		bookTicker := arbitrage.BookTicker{
			AskPrice: event.BestAskPrice,
			AskQty:   event.BestAskQty,
			BidPrice: event.BestBidPrice,
			BidQty:   event.BestBidQty,
			UpdateID: event.UpdateID,
		}

		if val, ok := ob.Symbols[event.Symbol]; ok {
			if event.BestAskPrice == val.BookTicker.AskPrice && event.BestBidPrice == val.BookTicker.BidPrice {
				return
			}
			ob.UpdateBookTicker(event.Symbol, &bookTicker)
			// InfoFmt("%s %s - Bid: %s (%s qty) Ask: %s (%s qty)", Yellow("Updated symbol"), val.Pair.String(), Green(bookTicker.BidPrice), Green(bookTicker.BidQty), Red(bookTicker.AskPrice), Red(bookTicker.AskQty))

			return
		}

		InfoFmt("Symbol %s not found.", event.Symbol)
	}

	errHandler := func(err error) {
		Error(err)
	}

	InfoFmt("Trying to connect to %d symbols", len(symbols))

	doneCh, stopCh, err := websocketStreamClient.WsCombinedBookTickerServe(symbols, wsBookTickerHandler, errHandler)
	if err != nil {
		Error(err)
		return
	}

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quitChannel
		InfoFmt("Received interrupt signal. Closing connection...")
		stopCh <- struct{}{} // use stopCh to stop streaming
	}()
	<-doneCh
}
