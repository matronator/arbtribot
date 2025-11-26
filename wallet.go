package main

import (
	"arbtribot/logger"
	"context"
	"strconv"

	binance "github.com/binance/binance-connector-go"
)

func WithdrawBTC(quantity string, address string) error {
	q, err := strconv.ParseFloat(quantity, 64)
	if err != nil {
		return err
	}

	withdraw, err := client.NewWithdrawService().Coin("BTC").Address(address).
		Amount(q).Do(context.Background())
	if err != nil {
		return err
	}

	logger.InfoFmt("%s", binance.PrettyPrint(withdraw))
	logger.InfoFmt("Withdrew %s BTC from wallet.", quantity)

	return err
}

func WithdrawalHistory() error {
	withdrawHistory, err := client.NewWithdrawHistoryService().Do(context.Background())
	if err != nil {
		logger.Error(err)
		return nil
	}
	logger.InfoFmt("%s", binance.PrettyPrint(withdrawHistory))

	return err
}
