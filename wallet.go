package main

import (
	"context"
	"strconv"

	binance "github.com/binance/binance-connector-go"
)

func WithdrawBTC(quantity string) error {
	q, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		return err
	}

	withdraw, err := client.NewWithdrawService().Coin("BTC").Address("bc1qlpft888fndt48fvz2c4dryndzu4657xdvedemq").
		Amount(q).Do(context.Background())
	if err != nil {
		return err
	}

	InfoFmt("%s", binance.PrettyPrint(withdraw))
	InfoFmt("Withdrew %s BTC from wallet.", quantity)

	return err
}

func WithdrawalHistory() error {
	withdrawHistory, err := client.NewWithdrawHistoryService().Do(context.Background())
	if err != nil {
		Error(err)
		return nil
	}
	InfoFmt("%s", binance.PrettyPrint(withdrawHistory))

	return err
}
