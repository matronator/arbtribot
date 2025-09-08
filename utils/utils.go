package utils

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"runtime"

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
