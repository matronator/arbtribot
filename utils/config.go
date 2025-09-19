package utils

import (
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog/log"
)

type Config struct {
	GeneralConfig  *GeneralConfig
	GridConfig     *GridConfig
	TriangleConfig *TriangleConfig
	NormalConfig   *NormalConfig
}

type GeneralConfig struct {
	SimulationMode bool    // true = simulate, false = live trading
	APIKey         string  // Binance API Key (for live trading)
	APISecret      string  // Binance API Secret (for live trading)
	FeeRate        float64 // Binance spot trading fee rate (default 0.001 = 0.1%)
	DebugMode      bool    // Enable debug logging
	VerboseLogging bool    // Enable very verbose logging (debug level)
	WdEnabled      bool
	TradingMode    string // "triangle", "grid" or "normal" - determines which trading strategy to use
}

type GridConfig struct {
	GridMinPriceChange float64 // Minimum price change to consider a trend (e.g., 0.005 for 0.5%)
	GridProfitTarget   float64 // Target profit percentage (e.g., 0.02 for 2%)
	GridStopLoss       float64 // Stop loss percentage (e.g., 0.01 for 1%)
	GridMaxHoldTime    int     // Maximum hold time in minutes
	GridMaxPositions   int     // Maximum number of concurrent positions
	GridCheckInterval  int     // Price check interval in seconds
}

type TriangleConfig struct {
	OrderUSDCAmount      float64  // Amount of USDC the trades will be executed for
	SimulationUSDCAmount float64  // Amount of USDC used for arbitrage detection calculations
	StartAsset           string   // Starting currency
	BaseAssets           []string // Base currencies
}

type NormalConfig struct {
	USDCAmount   float64  // Amount of USDC the trades will be executed for
	MaxPositions int      // Maximum number of concurrent positions
	StopLoss     float64  // Stop loss percentage (e.g., 0.01 for 1%)
	MinProfit    float64  // Minimum profit percentage (e.g., 0.01 for 1%)
	MaxHoldTime  int      // Maximum hold time in minutes
	TargetProfit float64  // Target profit percentage (e.g., 0.01 for 1%)
	BaseAsset    string   // Base coin
	QuoteAssets  []string // Quote coins
}

func LoadConfig() *Config {
	err := godotenv.Load(".env")
	if err != nil {
		log.Warn().Err(err).Msg("Error loading .env file, using defaults")
		return &Config{
			GeneralConfig: &GeneralConfig{
				SimulationMode: true, // Set to false for live trading
				APIKey:         "",
				APISecret:      "",
				FeeRate:        0.001, // 0.1% fee = 0.001
				DebugMode:      false, // Default to false, can be set via env
				VerboseLogging: false,
				WdEnabled:      false,
				TradingMode:    "grid", // Default to grid trading
			},
			GridConfig: &GridConfig{
				GridMinPriceChange: 0.005, // 0.5% minimum price change
				GridProfitTarget:   0.02,  // 2% profit target
				GridStopLoss:       0.01,  // 1% stop loss
				GridMaxHoldTime:    30,    // 30 minutes max hold time
				GridMaxPositions:   5,     // Maximum 5 concurrent positions
				GridCheckInterval:  10,    // Check every 10 seconds
			},
			TriangleConfig: &TriangleConfig{
				OrderUSDCAmount:      15,
				SimulationUSDCAmount: 5000,
				StartAsset:           "USDC",
				BaseAssets:           []string{"USDC", "BTC", "BNB", "ETH"},
			},
			NormalConfig: &NormalConfig{
				USDCAmount:   15,
				MaxPositions: 5,
				StopLoss:     0.01,
				MinProfit:    0.01,
				MaxHoldTime:  30,
				TargetProfit: 0.01,
				BaseAsset:    "USDC",
				QuoteAssets:  []string{"BTC", "BNB", "ETH"},
			},
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

	// Parse normal trading configs
	normalUSDCAmount, err := strconv.ParseFloat(os.Getenv("TRADING_USD_AMOUNT"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_USD_AMOUNT from .env file, using default 15 USDC")
		normalUSDCAmount = 15
	}

	normalMaxPositions, err := strconv.Atoi(os.Getenv("TRADING_MAX_POSITIONS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_MAX_POSITIONS from .env file, using default 5")
		normalMaxPositions = 5
	}

	normalStopLoss, err := strconv.ParseFloat(os.Getenv("TRADING_STOP_LOSS"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_STOP_LOSS from .env file, using default 0.01 (1%)")
		normalStopLoss = 0.01
	}

	normalMinProfit, err := strconv.ParseFloat(os.Getenv("TRADING_MIN_PROFIT"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_MIN_PROFIT from .env file, using default 0.01 (1%)")
		normalMinProfit = 0.01
	}

	normalMaxHoldTime, err := strconv.Atoi(os.Getenv("TRADING_MAX_HOLD_TIME"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_MAX_HOLD_TIME from .env file, using default 30 minutes")
		normalMaxHoldTime = 30
	}

	normalTargetProfit, err := strconv.ParseFloat(os.Getenv("TRADING_TARGET_PROFIT"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_TARGET_PROFIT from .env file, using default 0.01 (1%)")
		normalTargetProfit = 0.01
	}

	// Parse grid trading configs
	gridMinPriceChange, err := strconv.ParseFloat(os.Getenv("GRID_MIN_PRICE_CHANGE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_MIN_PRICE_CHANGE from .env file, using default 0.005 (0.5%)")
		gridMinPriceChange = 0.005
	}

	gridProfitTarget, err := strconv.ParseFloat(os.Getenv("GRID_PROFIT_TARGET"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_PROFIT_TARGET from .env file, using default 0.02 (2%)")
		gridProfitTarget = 0.02
	}

	gridStopLoss, err := strconv.ParseFloat(os.Getenv("GRID_STOP_LOSS"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_STOP_LOSS from .env file, using default 0.01 (1%)")
		gridStopLoss = 0.01
	}

	gridMaxHoldTime, err := strconv.Atoi(os.Getenv("GRID_MAX_HOLD_TIME"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_MAX_HOLD_TIME from .env file, using default 30 minutes")
		gridMaxHoldTime = 30
	}

	gridMaxPositions, err := strconv.Atoi(os.Getenv("GRID_MAX_POSITIONS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_MAX_POSITIONS from .env file, using default 5")
		gridMaxPositions = 5
	}

	gridCheckInterval, err := strconv.Atoi(os.Getenv("GRID_CHECK_INTERVAL"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing GRID_CHECK_INTERVAL from .env file, using default 10 seconds")
		gridCheckInterval = 10
	}

	return &Config{
		GeneralConfig: &GeneralConfig{
			SimulationMode: os.Getenv("SIMULATION_MODE") == "true",
			APIKey:         os.Getenv("BINANCE_API_KEY"),
			APISecret:      os.Getenv("BINANCE_SECRET_KEY"),
			FeeRate:        fee,
			DebugMode:      os.Getenv("DEBUG_MODE") == "true",
			VerboseLogging: os.Getenv("VERBOSE_LOGGING") == "true",
			WdEnabled:      os.Getenv("WD_ENABLED") == "true",
			TradingMode:    os.Getenv("TRADING_MODE"),
		},
		GridConfig: &GridConfig{
			GridMinPriceChange: gridMinPriceChange,
			GridProfitTarget:   gridProfitTarget,
			GridStopLoss:       gridStopLoss,
			GridMaxHoldTime:    gridMaxHoldTime,
			GridMaxPositions:   gridMaxPositions,
			GridCheckInterval:  gridCheckInterval,
		},
		TriangleConfig: &TriangleConfig{
			OrderUSDCAmount:      usdcAmount,
			SimulationUSDCAmount: simAmount,
			StartAsset:           os.Getenv("START_ASSET"),
			BaseAssets:           strings.Split(os.Getenv("BASE_ASSETS"), ","),
		},
		NormalConfig: &NormalConfig{
			USDCAmount:   normalUSDCAmount,
			MaxPositions: normalMaxPositions,
			StopLoss:     normalStopLoss,
			MinProfit:    normalMinProfit,
			MaxHoldTime:  normalMaxHoldTime,
			TargetProfit: normalTargetProfit,
			BaseAsset:    os.Getenv("TRADING_BASE_ASSET"),
			QuoteAssets:  strings.Split(os.Getenv("TRADING_QUOTE_ASSETS"), ","),
		},
	}
}
