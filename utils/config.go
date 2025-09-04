package utils

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

type Config struct {
	SimulationMode bool     // true = simulate, false = live trading
	APIKey         string   // Binance API Key (for live trading)
	APISecret      string   // Binance API Secret (for live trading)
	FeeRate        float64  // Binance spot trading fee rate (default 0.001 = 0.1%)
	DebugMode      bool     // Enable debug logging
	StartAsset     string   // Starting currency
	BaseAssets     []string // Base currencies
	WdEnabled      bool
}

func LoadConfig() *Config {
	err := godotenv.Load(".env")
	if err != nil {
		log.Warn().Err(err).Msg("Error loading .env file, using defaults")
		return &Config{
			SimulationMode: true, // Set to false for live trading
			APIKey:         "",
			APISecret:      "",
			FeeRate:        0.001, // 0.1% fee = 0.001
			DebugMode:      false, // Default to false, can be set via env
			StartAsset:     "USDC",
			BaseAssets:     []string{"USDC", "BTC", "BNB", "ETH"},
			WdEnabled:      false,
		}
	}

	fee, err := strconv.ParseFloat(os.Getenv("BINANCE_FEE_RATE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing BINANCE_FEE_RATE from .env file, using default 0.001 (0.1%% fee)")
		fee = 0.001 // Default to 0.1% fee
	}

	return &Config{
		SimulationMode: os.Getenv("SIMULATION_MODE") == "true",
		APIKey:         os.Getenv("BINANCE_API_KEY"),
		APISecret:      os.Getenv("BINANCE_SECRET_KEY"),
		FeeRate:        fee,
		DebugMode:      os.Getenv("DEBUG_MODE") == "true",
		StartAsset:     os.Getenv("START_ASSET"),
		BaseAssets:     strings.Split(os.Getenv("BASE_ASSETS"), ","),
		WdEnabled:      os.Getenv("WD_ENABLED") == "true",
	}
}
