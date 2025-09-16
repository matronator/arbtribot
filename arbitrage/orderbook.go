package arbitrage

import (
	"arbtribot/currency"
	"arbtribot/logger"
	"arbtribot/utils"
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	binance "github.com/binance/binance-connector-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/quagmt/udecimal"
	"github.com/rs/zerolog"
)

type Orderbook struct {
	Symbols     cmap.ConcurrentMap[string, *Symbol]
	Config      *utils.Config
	Client      *binance.Client
	TradeLogger *zerolog.Logger
}

type Symbol struct {
	Pair           currency.Pair
	BasePrecision  int8
	QuotePrecision int8
	Filter         ExchangeFilter
	bookData       atomic.Value // BookTicker
	LastUpdated    time.Time
	Triangles      []*Triangle
	Lock           sync.Mutex
}

func (s *Symbol) SetBookTicker(book *BookTicker) {
	s.bookData.Store(*book)
}

func (s *Symbol) GetBookTicker() *BookTicker {
	if v := s.bookData.Load(); v != nil {
		book := v.(BookTicker)
		return &book
	}
	return nil
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

func New(cfg *utils.Config, client *binance.Client, tradeLogger *zerolog.Logger) *Orderbook {
	symbols := cmap.New[*Symbol]()
	return &Orderbook{Symbols: symbols, Config: cfg, Client: client, TradeLogger: tradeLogger}
}

func (ob *Orderbook) Add(s *Symbol) {
	ob.Symbols.Set(s.Pair.String(), s)
}

func (ob *Orderbook) UpdateBookTicker(key string, book *BookTicker) (updated bool, err error) {
	if val, ok := ob.Symbols.Get(key); ok {
		err := val.Update(book, ob)
		if err != nil {
			return true, err
		}

		return true, nil
	}
	return false, fmt.Errorf("key %s doesn't exist on map Orderbook.Symbols", key)
}

func (s *Symbol) Update(book *BookTicker, ob *Orderbook) error {
	s.Lock.Lock()

	bookCopy := &BookTicker{
		BidPrice: book.BidPrice,
		BidQty:   book.BidQty,
		AskPrice: book.AskPrice,
		AskQty:   book.AskQty,
		UpdateID: book.UpdateID,
	}

	s.SetBookTicker(bookCopy)
	s.LastUpdated = time.Now()

	triangles := make([]*Triangle, len(s.Triangles))
	copy(triangles, s.Triangles)

	s.Lock.Unlock()

	for _, t := range triangles {
		err := t.updateTriangle(ob)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ob *Orderbook) FindTriangles() []*Triangle {
	var triangles []*Triangle
	var mu sync.Mutex
	var wg sync.WaitGroup

	for item := range ob.Symbols.IterBuffered() {
		startPair := item.Val
		startSymbol := item.Key
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
					Direction: "BUY",
				}
				firstCoin = startPair.Pair.Base
			} else {
				path1 = &Path{
					Pair:      startPair.Pair,
					Direction: "SELL",
				}
				firstCoin = startPair.Pair.Quote
			}
			// Don't set BookTicker here - it will be fetched fresh during simulation

			localTriangles := make([]*Triangle, 0)

			for midItem := range ob.Symbols.IterBuffered() {
				midSymbol := midItem.Key
				midPair := midItem.Val

				if midSymbol == startSymbol {
					continue
				}

				var path2 *Path
				var middleCoin currency.Currency

				if midPair.Pair.Base == firstCoin {
					path2 = &Path{
						Pair:      midPair.Pair,
						Direction: "SELL",
					}
					middleCoin = midPair.Pair.Quote
				} else if midPair.Pair.Quote == firstCoin {
					path2 = &Path{
						Pair:      midPair.Pair,
						Direction: "BUY",
					}
					middleCoin = midPair.Pair.Base
				} else {
					continue
				}
				// Don't set BookTicker here - it will be fetched fresh during simulation

				for endItem := range ob.Symbols.IterBuffered() {
					endSymbol := endItem.Key
					endPair := endItem.Val

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
							Direction: "SELL",
						}
					} else if endPair.Pair.Quote == middleCoin && endPair.Pair.Base == currency.USDC {
						path3 = &Path{
							Pair:      endPair.Pair,
							Direction: "BUY",
						}
					} else {
						continue
					}

					// Don't set BookTicker here - it will be fetched fresh during simulation

					triangle := &Triangle{
						PathA:      path1,
						PathB:      path2,
						PathC:      path3,
						OnCooldown: false,
						Lock:       sync.Cond{L: &sync.Mutex{}},
						mu:         sync.RWMutex{},
						priceCache: NewPriceCache(),
					}

					// Optional: Add triangles to symbols
					mu.Lock()
					startPair.Lock.Lock()
					startPair.Triangles = append(startPair.Triangles, triangle)
					startPair.Lock.Unlock()
					midPair.Lock.Lock()
					midPair.Triangles = append(midPair.Triangles, triangle)
					midPair.Lock.Unlock()
					endPair.Lock.Lock()
					endPair.Triangles = append(endPair.Triangles, triangle)
					endPair.Lock.Unlock()
					mu.Unlock()

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

// func (s *Symbol) UpdateSymbolTicker(book *BookTicker) {
// 	s.LastUpdated = time.Now()
// 	s.BookTicker = book
// 	for _, t := range s.Triangles {
// 		inTriangle := strings.Contains(t.String(), s.Pair.String())
// 		if !inTriangle {
// 			continue
// 		}
// 		paths := map[string]*Path{"A": t.PathA, "B": t.PathB, "C": t.PathC}
// 		for key, path := range paths {
// 			updateTriangle(t, key, book, ob)
// 		}
// 	}
// }

func FillOrderBook(client *binance.Client, cfg *utils.Config, tradeLogger *zerolog.Logger) (ob *Orderbook, err error) {
	info, err := client.NewExchangeInfoService().Do(context.Background())
	if err != nil {
		return nil, err
	}

	ob = New(cfg, client, tradeLogger)

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
			Lock:           sync.Mutex{},
		}

		ob.Add(&s)
	}

	return ob, nil
}

func (ob *Orderbook) FillPrices() (added int, err error) {
	file, err := os.OpenFile("orderbook.csv", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	file.WriteString("Symbol,BASE,QUOTE,basePrecision,quotePrecision,tickSize,stepSize,marketStepSize,minQty,marketMinQty,maxQty,marketMaxQty,minNotional,ASK,BID,updatedAt\n")

	res, err := ob.Client.NewTickerBookTickerService().Do(context.Background())
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
		pair, _ := ob.Symbols.Get(symbol.Symbol)
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

func (ob *Orderbook) UpdatePrices() (updated int, err error) {
	res, err := ob.Client.NewTickerBookTickerService().Do(context.Background())
	if err != nil {
		return 0, err
	}

	for _, symbol := range res {
		if val, ok := ob.Symbols.Get(symbol.Symbol); ok {
			bookTicker := val.GetBookTicker()
			if bookTicker == nil || (bookTicker.AskPrice != symbol.AskPrice || bookTicker.BidPrice != symbol.BidPrice) {
				var updateId int64
				if bookTicker != nil {
					updateId = bookTicker.UpdateID + 1
				} else {
					updateId = 0
				}
				book := &BookTicker{
					AskPrice: symbol.AskPrice,
					AskQty:   symbol.AskQty,
					BidPrice: symbol.BidPrice,
					BidQty:   symbol.BidQty,
					UpdateID: updateId,
				}

				_, err := ob.UpdateBookTicker(symbol.Symbol, book)
				if err != nil {
					return updated, err
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

	logger.Info(binance.PrettyPrint(res))
	logger.InfoFmt("Order for %s placed. %s %s %s for %s %s at price %s %s", logger.Cyan(symbol), dir, res.ExecutedQty, pair.Base, res.CummulativeQuoteQty, pair.Quote, res.Fills[0].Price, pair.Quote)

	return res, nil
}

func (s *Symbol) GetUSDPrice(ob *Orderbook) (string, error) {
	if s.Pair.Quote.String() == "USDC" {
		bookTicker := s.GetBookTicker()
		if bookTicker == nil {
			return "", fmt.Errorf("no book ticker data available for symbol %s", s.Pair.String())
		}
		return bookTicker.AskPrice, nil
	}

	c := s.Pair.Base.String()
	symbol := c + "USDC"
	if val, ok := ob.Symbols.Get(symbol); ok {
		bookTicker := val.GetBookTicker()
		if bookTicker == nil {
			return "", fmt.Errorf("no book ticker data available for symbol %s", symbol)
		}
		return bookTicker.AskPrice, nil
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
	if val, ok := ob.Symbols.Get(symbol); ok {
		quoteSymbol, _ := ob.Symbols.Get(quote + "USDC")
		bookTicker := quoteSymbol.GetBookTicker()
		if bookTicker == nil {
			return "", fmt.Errorf("no book ticker data available for symbol %s", quote+"USDC")
		}
		quotePriceS := bookTicker.AskPrice
		quotePrice, err := udecimal.Parse(quotePriceS)
		if err != nil {
			return "", err
		}

		valBookTicker := val.GetBookTicker()
		if valBookTicker == nil {
			return "", fmt.Errorf("no book ticker data available for symbol %s", symbol)
		}
		price, err := udecimal.Parse(valBookTicker.AskPrice)
		if err != nil {
			return "", err
		}

		return price.Mul(quotePrice).StringFixed(uint8(val.BasePrecision)), err
	}

	return "", fmt.Errorf("unknown symbol %s: ", symbol)
}
