package main

import (
	"arbitrage/arbitrage"
	"arbitrage/currency"
	"arbitrage/orderbook"
	"context"
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

func FillOrderBook() (added int, err error) {
	info, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	OrderBook = orderbook.New()

	for _, symbol := range info.Symbols {
		if _, ok := currency.AllSymbols[symbol.Symbol]; !ok {
			continue
		}

		f := orderbook.ExchangeFilter{}

		for _, filter := range symbol.Filters {
			switch filter.FilterType {
			case "PRICE_FILTER":
				f.MinPrice = filter.MinPrice
				f.MaxPrice = filter.MaxPrice
				f.TickSize = filter.TickSize
			case "LOT_SIZE":
				f.LotSize = orderbook.LotSize{
					MinQty:   filter.MinQty,
					MaxQty:   filter.MaxQty,
					StepSize: filter.StepSize,
				}
			case "MARKET_LOT_SIZE":
				f.MarketLotSize = orderbook.LotSize{
					MinQty:   filter.MinQty,
					MaxQty:   filter.MaxQty,
					StepSize: filter.StepSize,
				}
			case "MIN_NOTIONAL":
				f.MinNotional = filter.MinNotional
			default:
				continue
			}
		}

		s := orderbook.Symbol{
			Pair:           currency.AllSymbols[symbol.Symbol],
			BasePrecision:  int8(symbol.BaseAssetPrecision),
			QuotePrecision: int8(symbol.QuoteAssetPrecision),
			Filter:         f,
		}

		OrderBook.Add(s)
	}

	res, err := client.NewTickerBookTickerService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	for _, symbol := range res {
		if _, ok := currency.AllSymbols[symbol.Symbol]; !ok {
			continue
		}
		book := &orderbook.BookTicker{
			AskPrice: symbol.AskPrice,
			AskQty:   symbol.AskQty,
			BidPrice: symbol.BidPrice,
			BidQty:   symbol.BidQty,
			UpdateID: 0,
		}

		OrderBook.UpdateBookTicker(symbol.Symbol, book)
		added++
	}

	return
}

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
				triangle.PathA = arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "SELL",
				}
				sides[0] = 1
			} else if event.Symbol == currency.DOT_BTC.String() {
				triangle.PathB = arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "BUY",
				}
				sides[1] = 1
			} else if event.Symbol == currency.BTC_USDC.String() {
				triangle.PathC = arbitrage.Path{
					Pair:      currency.AllSymbols[event.Symbol],
					Ask:       bookTicker.AskPrice,
					Bid:       bookTicker.BidPrice,
					Direction: "SELL",
				}
				sides[2] = 1
			}

			if sides[0] == 1 && sides[1] == 1 && sides[2] == 1 {
				found, profit, err := triangle.CheckArbitrage(cfg)
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
