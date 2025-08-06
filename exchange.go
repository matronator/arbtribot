package main

import (
	"arbitrage/arbitrage"
	"arbitrage/currency"
	"arbitrage/orderbook"
	"time"

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

var OrderBook *orderbook.Orderbook

func ConnectToExchange() {
	websocketStreamClient := binance.NewWebsocketStreamClient(true)

	triangle := arbitrage.Triangle{}
	sides := []uint8{0, 0, 0}

	wsBookTickerHandler := func(event *binance.WsBookTickerEvent) {
		bookTicker := orderbook.BookTicker{
			AskPrice: event.BestAskPrice,
			AskQty:   event.BestAskQty,
			BidPrice: event.BestBidPrice,
			BidQty:   event.BestBidQty,
			UpdateID: event.UpdateID,
		}

		if val, ok := OrderBook.Symbols[event.Symbol]; ok {
			if event.BestAskPrice == val.BookTicker.AskPrice && event.BestBidPrice == val.BookTicker.BidPrice {
				return
			}
			OrderBook.UpdateBookTicker(event.Symbol, &bookTicker)
			InfoFmt("%s %s - Bid: %s (%s qty) Ask: %s (%s qty)", Yellow("Updated symbol"), val.Pair.String(), Green(bookTicker.BidPrice), Green(bookTicker.BidQty), Red(bookTicker.AskPrice), Red(bookTicker.AskQty))
			if event.Symbol == currency.DOT_USDC.String() {
				triangle.PathA = &arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "SELL",
				}
				sides[0] = 1
			} else if event.Symbol == currency.DOT_BTC.String() {
				triangle.PathB = &arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "BUY",
				}
				sides[1] = 1
			} else if event.Symbol == currency.BTC_USDC.String() {
				triangle.PathC = &arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "SELL",
				}
				sides[2] = 1
			}

			if sides[0] == 1 && sides[1] == 1 && sides[2] == 1 {
				found, profit, err := triangle.CheckArbitrage(cfg.FeeRate)
				if err != nil {
					Error(err)
				}
				if found {
					InfoFmt("%s", Green("Arbitrage found!"))
				}
				InfoFmt("Arbitrage profit: %f", profit)
			}
			return
		}

		InfoFmt("Symbol %s not found.", event.Symbol)
	}
	errHandler := func(err error) {
		Error(err)
	}

	// symbols := make([]string, 0)
	// i := 0
	// for k := range currency.AllSymbols {
	// 	symbols = append(symbols, k)
	// 	if i > 10 {
	// 		break;
	// 	}
	// 	i++;
	// }

	coins := arbitrage.TestArbitrage()
	symbols := []string{coins.PathA.Pair.String(), coins.PathB.Pair.String(), coins.PathC.Pair.String()}

	InfoFmt("Trying to connect to %d symbols", len(symbols))

	doneCh, stopCh, err := websocketStreamClient.WsCombinedBookTickerServe(symbols, wsBookTickerHandler, errHandler)
	if err != nil {
		Error(err)
		return
	}
	go func() {
		time.Sleep(120 * time.Second)
		stopCh <- struct{}{} // use stopCh to stop streaming
	}()
	<-doneCh
}
