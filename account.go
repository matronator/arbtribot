package main

import (
	"context"
	"strconv"

	binance "github.com/binance/binance-connector-go"
)

type Balances map[string]*binance.Balance

type OrderResponse struct {
	price string
}

func CheckAccountBalance() (Balances, error) {
	accountService := client.NewGetAccountService()
	res, err := accountService.Do(context.Background())
	if err != nil {
		return nil, err
	}

	balances := make(map[string]*binance.Balance)

	for _, balance := range res.Balances {
		amount, err := strconv.ParseFloat(balance.Free, 32)
		if err != nil {
			Error(err)
			continue
		}
		if amount > 0 {
			InfoFmt("Account has %s %s", balance.Free, balance.Asset)
			balances[balance.Asset] = &balance
		}
	}

	return balances, err
}

func ConvertAllToUSDC() error {
	newOrder, err := client.NewCreateOrderService().Symbol("BTCUSDC").
		Side("SELL").Type("MARKET").Quantity(0.00017).
		Do(context.Background())
	if err != nil {
		Error(err)
		return err
	}
	InfoFmt("%s", binance.PrettyPrint(newOrder))
	InfoFmt("Sold BTC for USDC")

	return err
}

func ConvertUSDToBNB(amount float64) error {
	newOrder, err := client.NewCreateOrderService().Symbol("BNBUSDC").
		Side("BUY").Type("MARKET").Quantity(amount).
		Do(context.Background())
	if err != nil {
		Error(err)
		return err
	}
	InfoFmt("%s", binance.PrettyPrint(newOrder))
	InfoFmt("Bought %f BNB for %s USDC", amount, newOrder.(binance.CreateOrderResponseFULL).CummulativeQuoteQty)

	return err
}
