package main

import (
	"context"
	"strconv"

	binance "github.com/binance/binance-connector-go"
)

type Balances map[string]*binance.Balance

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

func AllToUSDC() error {
	balances, err := CheckAccountBalance()
	if err != nil {
		return err
	}

	for s, b := range balances {
		if s == "BNB" {
			continue
		}
		err := ConvertToUSDC(s, b.Free)
		if err != nil {
			Error(err)
			continue
		}
	}

	InfoFmt("%s", Green("All assets changed to USDC"))

	return nil
}

func ConvertToUSDC(asset string, quantity string) error {
	q, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		return err
	}

	newOrder, err := client.NewCreateOrderService().Symbol(asset + "USDC").
		Side("SELL").Type("MARKET").Quantity(q).
		Do(context.Background())
	if err != nil {
		Error(err)
		return err
	}
	InfoFmt("%s", binance.PrettyPrint(newOrder))
	InfoFmt("Sold %s for USDC", asset)

	return err
}

func ConvertUSDCToBTC(quantity string) error {
	q, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		return err
	}

	newOrder, err := client.NewCreateOrderService().Symbol("BTCUSDC").
		Side("BUY").Type("MARKET").QuoteOrderQty(q).
		Do(context.Background())
	if err != nil {
		Error(err)
		return err
	}
	InfoFmt("%s", binance.PrettyPrint(newOrder))
	InfoFmt("Bought BTC for %s USDC", quantity)

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
