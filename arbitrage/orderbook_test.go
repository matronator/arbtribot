package arbitrage

import (
	"arbtribot/currency"
	"arbtribot/utils"
	"fmt"
	"testing"

	binance_connector "github.com/binance/binance-connector-go"
)

func TestUpdateBookTicker(t *testing.T) {
	client := binance_connector.NewClient("2F7nXbivEVT0OUZtTMW48JP7w5Qwcc6M6t2T5w84hyDL8JnYH166sWQZxmT7YWxe", "Gpm2aYBhkhk71LZ4UYIOy48TWlRXfpPVMD7RAPf1tb1piJgcct5WjJMihPDsn0WR")
	ob := New(utils.LoadConfig(), client)
	ob.Add(&Symbol{
		Pair: currency.BNB_USDC,
	})
	ob.UpdateBookTicker("bnbusdc", &BookTicker{
		BidPrice: "12",
		AskPrice: "23",
		BidQty:   "1",
		AskQty:   "2",
		UpdateID: 1,
	})
	ob.UpdateBookTicker("btcdot", &BookTicker{})
}

func TestUSDPrices(t *testing.T) {
	currency.FillPairs()
	client := binance_connector.NewClient("2F7nXbivEVT0OUZtTMW48JP7w5Qwcc6M6t2T5w84hyDL8JnYH166sWQZxmT7YWxe", "Gpm2aYBhkhk71LZ4UYIOy48TWlRXfpPVMD7RAPf1tb1piJgcct5WjJMihPDsn0WR")
	ob, err := FillOrderBook(client, utils.LoadConfig())
	if err != nil {
		fmt.Println("Error filling orderbook")
		fmt.Printf("%s\n", err)
		return
	}

	_, err = ob.FillPrices(client)
	if err != nil {
		fmt.Println("Error filling prices")
		fmt.Printf("%s\n", err)
		return
	}

	symbols := []string{"DOTBTC", "ADAETH", "LTCBTC", "XRPBNB", "STXBTC"}

	for _, symbol := range symbols {
		s := ob.Symbols[symbol]
		price, err := s.GetUSDPrice(ob)
		if err != nil {
			fmt.Println("Error converting price")
			fmt.Printf("%s\n", err)
			continue
		}

		from := ob.Symbols[symbol].Pair.Base
		usdcPair := ob.Symbols[from.String()+"USDC"]

		fmt.Printf("Symbol: %s -> Calculated: %s = Real: %s\n", symbol, price, usdcPair.BookTicker.AskPrice)
	}
}
