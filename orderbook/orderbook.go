package orderbook

import (
	"arbitrage/currency"
	"context"
	"fmt"
	"os"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
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
	s.LastUpdated = time.Now()
	s.BookTicker = &book
}

func FillOrderBook(client *binance.Client) (ob *Orderbook, err error) {
	info, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	ob = New()

	for _, symbol := range info.Symbols {
		if _, ok := currency.AllSymbols[symbol.Symbol]; !ok {
			continue
		}

		f := ExchangeFilter{}

		for _, filter := range symbol.Filters {
			switch filter.FilterType {
			case "PRICE_FILTER":
				f.MinPrice = filter.MinPrice
				f.MaxPrice = filter.MaxPrice
				f.TickSize = filter.TickSize
			case "LOT_SIZE":
				f.LotSize = LotSize{
					MinQty:   filter.MinQty,
					MaxQty:   filter.MaxQty,
					StepSize: filter.StepSize,
				}
			case "MARKET_LOT_SIZE":
				f.MarketLotSize = LotSize{
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

		s := Symbol{
			Pair:           currency.AllSymbols[symbol.Symbol],
			BasePrecision:  int8(symbol.BaseAssetPrecision),
			QuotePrecision: int8(symbol.QuoteAssetPrecision),
			Filter:         f,
		}

		ob.Add(s)
	}

	return ob, nil
}

func (ob *Orderbook) FillPrices(client *binance.Client) (added int, err error) {
	file, err := os.OpenFile("orderbook.csv", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	file.WriteString("Symbol,BASE,QUOTE,basePrecision,quotePrecision,tickSize,stepSize,minQty,maxQty,minNotional,ASK,BID,updatedAt\n")

	res, err := client.NewTickerBookTickerService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	for _, symbol := range res {
		if _, ok := currency.AllSymbols[symbol.Symbol]; !ok {
			continue
		}
		book := &BookTicker{
			AskPrice: symbol.AskPrice,
			AskQty:   symbol.AskQty,
			BidPrice: symbol.BidPrice,
			BidQty:   symbol.BidQty,
			UpdateID: 0,
		}

		ob.UpdateBookTicker(symbol.Symbol, book)
		pair := ob.Symbols[symbol.Symbol]
		fmt.Fprintf(file, "%s,%s,%s,%d,%d,%s,%s,%s,%s,%s,%s,%s,%s\n", symbol.Symbol, pair.Pair.From, pair.Pair.To, pair.BasePrecision, pair.QuotePrecision, pair.Filter.TickSize, pair.Filter.LotSize.StepSize, pair.Filter.LotSize.MinQty, pair.Filter.LotSize.MaxQty, pair.Filter.MinNotional, book.AskPrice, book.BidPrice, pair.LastUpdated.Format("2006-01-02 15:04:05"))
		added++
	}

	return
}

func (s *Symbol) GetUSDPrice(ob *Orderbook) (string, error) {
	if s.Pair.To.String() == "USDC" {
		return s.BookTicker.AskPrice, nil
	}

	c := s.Pair.From.String()
	symbol := c + "USDC"
	if val, ok := ob.Symbols[symbol]; ok {
		return val.BookTicker.AskPrice, nil
	}

	for _, base := range currency.BaseCurrencies {
		if base.String() == "USDC" {
			continue
		}

		price, err := checkSymbol(c, base.String(), ob)
		if err != nil {
			fmt.Print(err)
			continue
		}

		return price, nil
	}

	return "", fmt.Errorf("unknown price")
}

func checkSymbol(base string, quote string, ob *Orderbook) (string, error) {
	symbol := base + quote
	if val, ok := ob.Symbols[symbol]; ok {
		quotePriceS := ob.Symbols[quote+"USDC"].BookTicker.AskPrice
		quotePrice, err := udecimal.Parse(quotePriceS)
		if err != nil {
			return "", err
		}

		price, err := udecimal.Parse(val.BookTicker.AskPrice)
		if err != nil {
			return "", err
		}

		return price.Mul(quotePrice).StringFixed(uint8(val.BasePrecision)), err
	}

	return "", fmt.Errorf("unknown symbol %s: ", symbol)
}
