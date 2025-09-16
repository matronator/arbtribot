package utils

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

type Config struct {
	SimulationMode       bool     // true = simulate, false = live trading
	APIKey               string   // Binance API Key (for live trading)
	APISecret            string   // Binance API Secret (for live trading)
	FeeRate              float64  // Binance spot trading fee rate (default 0.001 = 0.1%)
	OrderUSDCAmount      float64  // Amount of USDC the trades will be executed for
	SimulationUSDCAmount float64  // Amount of USDC used for arbitrage detection calculations
	DebugMode            bool     // Enable debug logging
	VerboseLogging       bool     // Enable very verbose logging (debug level)
	StartAsset           string   // Starting currency
	BaseAssets           []string // Base currencies
	WdEnabled            bool
}

func LoadConfig() *Config {
	err := godotenv.Load(".env")
	if err != nil {
		log.Warn().Err(err).Msg("Error loading .env file, using defaults")
		return &Config{
			SimulationMode:       true, // Set to false for live trading
			APIKey:               "",
			APISecret:            "",
			FeeRate:              0.001, // 0.1% fee = 0.001
			OrderUSDCAmount:      15,
			SimulationUSDCAmount: 5000,
			DebugMode:            false, // Default to false, can be set via env
			VerboseLogging:       false,
			StartAsset:           "USDC",
			BaseAssets:           []string{"USDC", "BTC", "BNB", "ETH"},
			WdEnabled:            false,
		}
	}

	fee, err := strconv.ParseFloat(os.Getenv("BINANCE_FEE_RATE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing BINANCE_FEE_RATE from .env file, using default 0.001 (0.1%% fee)")
		fee = 0.001 // Default to 0.1% fee
	}

	usdcAmount, err := strconv.ParseFloat(os.Getenv("ORDER_USDC_AMOUNT"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing ORDER_USDC_AMOUNT from .env file, using default 15 USDC")
		usdcAmount = 15
	}

	simAmount, err := strconv.ParseFloat(os.Getenv("SIMULATION_USDC_AMOUNT"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing SIMULATION_USDC_AMOUNT from .env file, using default 5000 USDC")
		simAmount = 5000
	}

	return &Config{
		SimulationMode:       os.Getenv("SIMULATION_MODE") == "true",
		APIKey:               os.Getenv("BINANCE_API_KEY"),
		APISecret:            os.Getenv("BINANCE_SECRET_KEY"),
		FeeRate:              fee,
		OrderUSDCAmount:      usdcAmount,
		SimulationUSDCAmount: simAmount,
		DebugMode:            os.Getenv("DEBUG_MODE") == "true",
		VerboseLogging:       os.Getenv("VERBOSE_LOGGING") == "true",
		StartAsset:           os.Getenv("START_ASSET"),
		BaseAssets:           strings.Split(os.Getenv("BASE_ASSETS"), ","),
		WdEnabled:            os.Getenv("WD_ENABLED") == "true",
	}
}
