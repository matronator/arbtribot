package utils

import (
	"arbtribot/logger"
	"bufio"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"

	binance "github.com/binance/binance-connector-go"
)

func ReadLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	return lines, scanner.Err()
}

func GetRoot() string {
	_, b, _, _ := runtime.Caller(0)
	return filepath.Dir(b + "/../../")
}

func CheckUSDCBalance(client *binance.Client) (*binance.Balance, error) {
	accountService := client.NewGetAccountService()
	res, err := accountService.Do(context.Background())
	if err != nil {
		return nil, err
	}

	for _, balance := range res.Balances {
		if balance.Asset == "USDC" {
			return &balance, err
		}
	}

	return nil, err
}

func CheckAccountBalances(client *binance.Client, assets []string) (map[string]binance.Balance, error) {
	service := client.NewGetAccountService()
	res, err := service.Do(context.Background())
	if err != nil {
		return nil, err
	}

	balances := make(map[string]binance.Balance)

	for _, balance := range res.Balances {
		if !slices.Contains(assets, balance.Asset) {
			continue
		}

		amount, err := strconv.ParseFloat(balance.Free, 32)
		if err != nil {
			logger.Error(err)
			continue
		}
		if amount > 0 {
			logger.InfoFmt("Account has %s %s", balance.Free, balance.Asset)
			balances[balance.Asset] = balance
		}
	}

	return balances, err
}

func GoroutineId() uint64 {
	b := make([]byte, 64)
	b = b[:runtime.Stack(b, false)]
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	n, _ := strconv.ParseUint(string(b), 10, 64)
	return n
}
