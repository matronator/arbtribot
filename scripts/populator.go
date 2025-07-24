package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	binance "github.com/binance/binance-connector-go"
	"github.com/joho/godotenv"
)

func main() {
	// db, err := sql.Open("mysql", "root:rootroot@tcp(localhost:3306)/arbtribot?parseTime=true")
	// if err != nil {
	// 	panic(err)
	// }

	// defer db.Close()

	file, err := os.OpenFile("pairs.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer file.Close()

	ethFile, err := os.OpenFile("pairs_eth.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer ethFile.Close()

	btcFile, err := os.OpenFile("pairs_btc.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer btcFile.Close()

	usdcFile, err := os.OpenFile("pairs_usdc.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer usdcFile.Close()

	bnbFile, err := os.OpenFile("pairs_bnb.txt", os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		panic(err)
	}
	defer bnbFile.Close()

	err = godotenv.Load(".env")
	if err != nil {
		panic("Error loading .env file")
	}

	apiKey := os.Getenv("BINANCE_API_KEY")
	apiSecret := os.Getenv("BINANCE_SECRET_KEY")

	// coins := currency.Currencies
	tickersRes, err := GetTickers(apiKey, apiSecret)
	if err != nil {
		fmt.Println("Error fetching tickers:", err)
		return
	}

	// tickers := make([]string, 0, len(tickersRes))
	for _, ticker := range tickersRes.Symbols {
		// tickers = append(tickers, ticker.Symbol)
		if (ticker.Status != "TRADING") {
			continue
		}
		if (strings.HasSuffix(ticker.Symbol, "ETH")) {
			_, err := ethFile.WriteString(ticker.Symbol + "\n")
			if err != nil {
				fmt.Println("Error writing to file:", err)
				continue
			}
		} else if (strings.HasSuffix(ticker.Symbol, "BTC")) {
			_, err := btcFile.WriteString(ticker.Symbol + "\n")
			if err != nil {
				fmt.Println("Error writing to file:", err)
				continue
			}
		} else if (strings.HasSuffix(ticker.Symbol, "USDC")) {
			_, err := usdcFile.WriteString(ticker.Symbol + "\n")
			if err != nil {
				fmt.Println("Error writing to file:", err)
				continue
			}
		} else if (strings.HasSuffix(ticker.Symbol, "BNB")) {
			_, err := bnbFile.WriteString(ticker.Symbol + "\n")
			if err != nil {
				fmt.Println("Error writing to file:", err)
				continue
			}
		}
	}

	// for _, coin := range coins {
	// 	for _, base := range currency.BaseCurrencies {
	// 		if base.Symbol == coin.Symbol {
	// 			continue
	// 		}

	// 		fmt.Println("Checking pair:", coin.Symbol, base.Symbol)

	// 		i := slices.Index(tickers, coin.Symbol+base.Symbol)
	// 		if i == -1 {
	// 			fmt.Println("Pair not found:", coin.Symbol+base.Symbol)
	// 			continue
	// 		}

	// 		ticker := tickers[i]

	// 		fmt.Printf("Found pair: %s with price %s\n", coin.Symbol+base.Symbol, ticker)
	// 		_, err := file.WriteString(coin.Symbol + base.Symbol + "\n")
	// 		if err != nil {
	// 			fmt.Println("Error writing to file:", err)
	// 			continue
	// 		}
	// 	}
	// }
}

func GetTickers(apiKey string, apiSecret string) (ticker *binance.ExchangeInfoResponse, err error) {
	baseURL := "https://api.binance.com"

	client := binance.NewClient(apiKey, apiSecret, baseURL)

	res, err2 := client.NewExchangeInfoService().Do(context.Background())
	if err2 != nil {
		fmt.Println(err2)
		return nil, err2
	}

	return res, nil
}
