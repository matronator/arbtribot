package main

import (
	binance "github.com/binance/binance-connector-go"
)

func Test(cfg Config) {
	client := binance.NewWebsocketAPIClient(cfg.APIKey, cfg.APISecret)

	err := client.Connect()
	if err != nil {
		Error("Error: %v", err)
		return
	}
	defer client.Close()
}
