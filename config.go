package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

type Config struct {
	SimulationMode bool   // true = simulate, false = live trading
	APIKey         string // Binance API Key (for live trading)
	APISecret      string // Binance API Secret (for live trading)
	FeeRate        float64 // Binance spot trading fee rate (default 0.001 = 0.1%)
}

func LoadConfig() *Config {
	err := godotenv.Load(".env")
	if err != nil {
		fmt.Println("Error loading .env file, using defaults")
		return &Config{
			SimulationMode: true,     // Set to false for live trading
			APIKey:         "",
			APISecret:      "",
			FeeRate:        0.001,    // 0.1% fee = 0.001
		}
	}

	fee, err := strconv.ParseFloat(os.Getenv("BINANCE_FEE_RATE"), 64);
	if err != nil {
		fmt.Println("Error parsing BINANCE_FEE_RATE from .env file, using default 0.001 (0.1%% fee)")
		fee = 0.001 // Default to 0.1% fee
	}

	return &Config{
		SimulationMode: os.Getenv("SIMULATION_MODE") == "true",
		APIKey:         os.Getenv("BINANCE_API_KEY"),
		APISecret:      os.Getenv("BINANCE_SECRET_KEY"),
		FeeRate:        fee,
	}
}
