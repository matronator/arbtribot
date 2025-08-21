package arbitrage

import (
	"arbitrage/currency"
	"arbitrage/logger"
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	binance "github.com/binance/binance-connector-go"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog/log"
)

type Orderbook struct {
	Symbols map[string]*Symbol
}

type Symbol struct {
	Pair           currency.Pair
	BasePrecision  int8
	QuotePrecision int8
	Filter         ExchangeFilter
	BookTicker     *BookTicker
	LastUpdated    time.Time
	Paths          []*Path
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

func New() *Orderbook {
	symbols := make(map[string]*Symbol)
	return &Orderbook{Symbols: symbols}
}

func (ob *Orderbook) Add(s *Symbol) {
	ob.Symbols[s.Pair.String()] = s
}

func (ob *Orderbook) UpdateBookTicker(key string, book *BookTicker) (updated bool, err error) {
	if val, ok := ob.Symbols[key]; ok {
		val.BookTicker = book
		for _, path := range val.Paths {
			path.Ask = book.AskPrice
			path.Bid = book.BidPrice
		}

		return true, err
	}
	return false, fmt.Errorf("key %s doesn't exist on map Orderbook.Symbols", key)
}

func (ob *Orderbook) FindTriangles(fee float64) []*Triangle {
	var triangles []*Triangle
	var mu sync.Mutex
	var wg sync.WaitGroup

	for startSymbol, startPair := range ob.Symbols {
		// Only consider pairs involving USDC
		if startPair.Pair.Base != currency.USDC && startPair.Pair.Quote != currency.USDC {
			continue
		}

		wg.Add(1)

		go func(startSymbol string, startPair *Symbol) {
			defer wg.Done()

			var path1 *Path
			var firstCoin currency.Currency

			if startPair.Pair.Quote == currency.USDC {
				path1 = &Path{
					Pair:      startPair.Pair,
					Ask:       startPair.BookTicker.AskPrice,
					Bid:       startPair.BookTicker.BidPrice,
					Direction: "BUY",
				}
				firstCoin = startPair.Pair.Base
			} else {
				path1 = &Path{
					Pair:      startPair.Pair,
					Ask:       startPair.BookTicker.AskPrice,
					Bid:       startPair.BookTicker.BidPrice,
					Direction: "SELL",
				}
				firstCoin = startPair.Pair.Quote
			}

			localTriangles := make([]*Triangle, 0)

			for midSymbol, midPair := range ob.Symbols {
				if midSymbol == startSymbol {
					continue
				}

				var path2 *Path
				var middleCoin currency.Currency

				if midPair.Pair.Base == firstCoin {
					path2 = &Path{
						Pair:      midPair.Pair,
						Ask:       midPair.BookTicker.AskPrice,
						Bid:       midPair.BookTicker.BidPrice,
						Direction: "SELL",
					}
					middleCoin = midPair.Pair.Quote
				} else if midPair.Pair.Quote == firstCoin {
					path2 = &Path{
						Pair:      midPair.Pair,
						Ask:       midPair.BookTicker.AskPrice,
						Bid:       midPair.BookTicker.BidPrice,
						Direction: "BUY",
					}
					middleCoin = midPair.Pair.Base
				} else {
					continue
				}

				for endSymbol, endPair := range ob.Symbols {
					if endSymbol == startSymbol || endSymbol == midSymbol {
						continue
					}

					if endPair.Pair.Base != currency.USDC && endPair.Pair.Quote != currency.USDC {
						continue
					}

					var path3 *Path

					if endPair.Pair.Base == middleCoin && endPair.Pair.Quote == currency.USDC {
						path3 = &Path{
							Pair:      endPair.Pair,
							Ask:       endPair.BookTicker.AskPrice,
							Bid:       endPair.BookTicker.BidPrice,
							Direction: "SELL",
						}
					} else if endPair.Pair.Quote == middleCoin && endPair.Pair.Base == currency.USDC {
						path3 = &Path{
							Pair:      endPair.Pair,
							Ask:       endPair.BookTicker.AskPrice,
							Bid:       endPair.BookTicker.BidPrice,
							Direction: "BUY",
						}
					} else {
						continue
					}

					triangle := &Triangle{
						PathA: path1,
						PathB: path2,
						PathC: path3,
					}

					// Optional: Add paths to symbols
					ob.Symbols[startSymbol].Paths = append(ob.Symbols[startSymbol].Paths, path1)
					ob.Symbols[midSymbol].Paths = append(ob.Symbols[midSymbol].Paths, path2)
					ob.Symbols[endSymbol].Paths = append(ob.Symbols[endSymbol].Paths, path3)

					localTriangles = append(localTriangles, triangle)
				}
			}

			// Merge local triangles into global list
			mu.Lock()
			triangles = append(triangles, localTriangles...)
			mu.Unlock()
		}(startSymbol, startPair)
	}

	wg.Wait()
	return triangles
}

func (s *Symbol) UpdateSymbolTicker(book *BookTicker) {
	s.LastUpdated = time.Now()
	s.BookTicker = book
	for _, path := range s.Paths {
		path.Ask = book.AskPrice
		path.Bid = book.BidPrice
	}
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
			case "NOTIONAL":
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

		ob.Add(&s)
	}

	return ob, nil
}

func (ob *Orderbook) FillPrices(client *binance.Client) (added int, err error) {
	file, err := os.OpenFile("orderbook.csv", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	file.WriteString("Symbol,BASE,QUOTE,basePrecision,quotePrecision,tickSize,stepSize,marketStepSize,minQty,marketMinQty,maxQty,marketMaxQty,minNotional,ASK,BID,updatedAt\n")

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
		fmt.Fprintf(file, "%s,%s,%s,%d,%d,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			symbol.Symbol,
			pair.Pair.Base,
			pair.Pair.Quote,
			pair.BasePrecision,
			pair.QuotePrecision,
			pair.Filter.TickSize,
			pair.Filter.LotSize.StepSize,
			pair.Filter.MarketLotSize.StepSize,
			pair.Filter.LotSize.MinQty,
			pair.Filter.MarketLotSize.MinQty,
			pair.Filter.LotSize.MaxQty,
			pair.Filter.MarketLotSize.MaxQty,
			pair.Filter.MinNotional,
			book.AskPrice,
			book.BidPrice,
			pair.LastUpdated.Format("2006-01-02 15:04:05"))
		added++
	}

	return
}

func (ob *Orderbook) UpdatePrices(client *binance.Client) (updated int, err error) {
	res, err := client.NewTickerBookTickerService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	for _, symbol := range res {
		if val, ok := ob.Symbols[symbol.Symbol]; ok {
			if val.BookTicker.AskPrice != symbol.AskPrice || val.BookTicker.BidPrice != symbol.BidPrice {
				book := &BookTicker{
					AskPrice: symbol.AskPrice,
					AskQty:   symbol.AskQty,
					BidPrice: symbol.BidPrice,
					BidQty:   symbol.BidQty,
					UpdateID: val.BookTicker.UpdateID + 1,
				}

				ob.UpdateBookTicker(symbol.Symbol, book)

				for _, path := range val.Paths {
					path.Ask = symbol.AskPrice
					path.Bid = symbol.BidPrice
				}

				updated++
			}
		}
	}

	return
}

func NewMarketOrder(client *binance.Client, pair *currency.Pair, dir string, qty float64) (*binance.CreateOrderResponseFULL, error) {
	symbol := pair.String()

	order, err := client.NewCreateOrderService().Symbol(symbol).
		Quantity(qty).Type("MARKET").Side(dir).Do(context.Background())
	if err != nil {
		return nil, err
	}

	res := order.(*binance.CreateOrderResponseFULL)

	log.Info().Msg(binance.PrettyPrint(res))
	log.Info().Msgf("Order for %s placed. %s %s %s for %s %s", logger.Cyan(symbol), dir, res.ExecutedQty, pair.Base, res.CummulativeQuoteQty, pair.Quote)

	return res, nil
}

func (s *Symbol) GetUSDPrice(ob *Orderbook) (string, error) {
	if s.Pair.Quote.String() == "USDC" {
		return s.BookTicker.AskPrice, nil
	}

	c := s.Pair.Base.String()
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
