package main

import (
	"arbtribot/arbitrage"
	"arbtribot/logger"
	"os"
	"os/signal"
	"syscall"

	binance "github.com/binance/binance-connector-go"
)

func Test() {
	client := binance.NewWebsocketAPIClient(cfg.APIKey, cfg.APISecret)

	err := client.Connect()
	if err != nil {
		logger.Error(err)
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
			// logger.InfoFmt("%s %s - Bid: %s (%s qty) Ask: %s (%s qty)", logger.Yellow("Updated symbol"), val.Pair.String(), logger.Green(bookTicker.BidPrice), logger.Green(bookTicker.BidQty), logger.Red(bookTicker.AskPrice), logger.Red(bookTicker.AskQty))

			return
		}

		logger.InfoFmt("Symbol %s not found.", event.Symbol)
	}

	errHandler := func(err error) {
		logger.Error(err)
	}

	logger.InfoFmt("Trying to connect to %d symbols", len(symbols))

	doneCh, stopCh, err := websocketStreamClient.WsCombinedBookTickerServe(symbols, wsBookTickerHandler, errHandler)
	if err != nil {
		logger.Error(err)
		return
	}

	logger.InfoFmt("Connected!")

	quitChannel := make(chan os.Signal, 1)
	signal.Notify(quitChannel, syscall.SIGINT, syscall.SIGTERM)

	logger.InfoFmt("Listening to websocket updates for symbols: %v", symbols)

	go func() {
		<-quitChannel
		logger.InfoFmt("Received interrupt signal. Closing connection...")
		stopCh <- struct{}{} // use stopCh to stop streaming
	}()
	<-doneCh
}
