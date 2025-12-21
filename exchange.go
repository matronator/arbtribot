package main

import (
	"arbtribot/arbitrage"
	"arbtribot/logger"

	binance "github.com/binance/binance-connector-go"
)

func Test() {
	client := binance.NewWebsocketAPIClient(cfg.GeneralConfig.APIKey, cfg.GeneralConfig.APISecret)

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
		pricesChanged := true
		if currentBook != nil {
			if event.BestAskPrice == currentBook.AskPrice && event.BestBidPrice == currentBook.BidPrice {
				pricesChanged = false
			}
		}

		bookCopy := &arbitrage.BookTicker{
			AskPrice: bookTicker.AskPrice,
			AskQty:   bookTicker.AskQty,
			BidPrice: bookTicker.BidPrice,
			BidQty:   bookTicker.BidQty,
			UpdateID: bookTicker.UpdateID,
		}

		// Always update to refresh LastUpdated timestamp, even if prices haven't changed
		// This ensures we know the websocket connection is still alive
		updated, err := h.orderbook.UpdateBookTicker(event.Symbol, bookCopy)
		if err != nil {
			logger.Error(err)
			return
		}

		if !updated {
			logger.WarningFmt("Symbol %s couldn't update book ticker.", val.Pair.String())
			return
		}

		// Debug: Log price updates (only when prices actually changed)
		if pricesChanged && h.orderbook.Config.GeneralConfig.TraceLogging {
			logger.TraceFmt("Updated %s: Ask=%s, Bid=%s", event.Symbol, bookCopy.AskPrice, bookCopy.BidPrice)
		}

		return
	}

	logger.WarningFmt("Symbol %s not found.", event.Symbol)
}
