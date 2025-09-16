package main

import (
	"arbtribot/arbitrage"
	"arbtribot/logger"

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

type BookTickerHandler struct {
	orderbook *arbitrage.Orderbook
}

func NewBookTickerHandler(orderbook *arbitrage.Orderbook) *BookTickerHandler {
	return &BookTickerHandler{
		orderbook: orderbook,
	}
}

func (h *BookTickerHandler) HandleError(err error) {
	logger.Error(err)
}

func (h *BookTickerHandler) HandleBookTickerEvent(event *binance.WsBookTickerEvent) {
	bookTicker := arbitrage.BookTicker{
		AskPrice: event.BestAskPrice,
		AskQty:   event.BestAskQty,
		BidPrice: event.BestBidPrice,
		BidQty:   event.BestBidQty,
		UpdateID: event.UpdateID,
	}

	if val, ok := h.orderbook.Symbols.Get(event.Symbol); ok {
		currentBook := val.GetBookTicker()
		if currentBook != nil {
			if event.BestAskPrice == currentBook.AskPrice && event.BestBidPrice == currentBook.BidPrice {
				return
			}
		}

		bookCopy := &arbitrage.BookTicker{
			AskPrice: bookTicker.AskPrice,
			AskQty:   bookTicker.AskQty,
			BidPrice: bookTicker.BidPrice,
			BidQty:   bookTicker.BidQty,
			UpdateID: bookTicker.UpdateID,
		}

		updated, err := h.orderbook.UpdateBookTicker(event.Symbol, bookCopy)
		if err != nil {
			logger.Error(err)
			return
		}

		if !updated {
			logger.WarningFmt("Symbol %s couldn't update book ticker.", val.Pair.String())
			return
		}

		// Debug: Log price updates
		if h.orderbook.Config.VerboseLogging {
			logger.DebugFmt("Updated %s: Ask=%s, Bid=%s", event.Symbol, bookCopy.AskPrice, bookCopy.BidPrice)
		}

		return
	}

	logger.WarningFmt("Symbol %s not found.", event.Symbol)
}
