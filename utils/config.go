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
	NormalConfig   *TradingConfig
	FuturesConfig  *FuturesConfig
	MarginConfig   *MarginConfig
}

type GeneralConfig struct {
	SimulationMode bool    // true = simulate, false = live trading
	APIKey         string  // Binance API Key (for live trading)
	APISecret      string  // Binance API Secret (for live trading)
	FeeRate        float64 // Binance spot trading fee rate (default 0.001 = 0.1%)
	DebugMode      bool    // Enable debug logging
	LogToConsole   bool    // Enable logging to console (true = console, false = file)
	VerboseLogging bool    // Enable very verbose logging (debug level)
	TraceLogging   bool    // Enable trace logging (trace level)
	WdEnabled      bool
	TradingMode    string // "triangle", "grid" or "normal" - determines which trading strategy to use
	Futures        bool   // true = futures trading, false = spot trading
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

type TradingConfig struct {
	USDCAmount   float64  // Amount of USDC the trades will be executed for
	MaxPositions int      // Maximum number of concurrent positions
	StopLoss     float64  // Stop loss percentage (e.g., 0.01 for 1%)
	MinProfit    float64  // Minimum profit percentage (e.g., 0.01 for 1%)
	MaxHoldTime  int      // Maximum hold time in minutes
	TargetProfit float64  // Target profit percentage (e.g., 0.01 for 1%)
	BaseAsset    string   // Base coin
	QuoteAssets  []string // Quote coins
}

// FuturesConfig holds configuration for the directional futures strategy.
type FuturesConfig struct {
	USDTPositionSize float64  // Notional size per position (in USDT)
	MaxPositions     int      // Maximum concurrent futures positions
	StopLoss         float64  // Max loss (as fraction) before closing
	TrailingStart    float64  // Profit threshold that enables trailing exit
	TrailingGap      float64  // Allowed pullback (as fraction) after trailing start
	EntryChange      float64  // Minimum absolute price move to open a position
	LookbackPoints   int      // History points to evaluate momentum
	Leverage         int      // Futures leverage to apply in PnL calculation
	CheckInterval    int      // Seconds between evaluation cycles
	BaseAsset        string   // Futures quote asset, e.g., USDT
	QuoteAssets      []string // Coins to trade against the base asset
}

// MarginConfig holds configuration for spot margin trading strategy.
type MarginConfig struct {
	USDTPositionSize float64  // Notional size per position (in USDT)
	MaxPositions     int      // Maximum concurrent margin positions
	StopLoss         float64  // Max loss (as fraction) before closing
	TrailingStart    float64  // Profit threshold that enables trailing exit
	TrailingGap      float64  // Allowed pullback (as fraction) after trailing start
	EntryChange      float64  // Minimum absolute price move to open a position
	LookbackPoints   int      // History points to evaluate momentum
	CheckInterval    int      // Seconds between evaluation cycles
	BaseAsset        string   // Margin quote asset, e.g., USDT
	QuoteAssets      []string // Coins to trade against the base asset
	MarginType       string   // "ISOLATED" or "CROSS" margin type
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
				LogToConsole:   true,  // Default to false, can be set via env
				TraceLogging:   false, // Default to false, can be set via env
				VerboseLogging: false,
				WdEnabled:      false,
				TradingMode:    "grid", // Default to grid trading
				Futures:        false,
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
			NormalConfig: &TradingConfig{
				USDCAmount:   15,
				MaxPositions: 5,
				StopLoss:     0.01,
				MinProfit:    0.01,
				MaxHoldTime:  30,
				TargetProfit: 0.01,
				BaseAsset:    "USDC",
				QuoteAssets:  []string{"BTC", "BNB", "ETH"},
			},
			FuturesConfig: &FuturesConfig{
				USDTPositionSize: 25,
				MaxPositions:     3,
				StopLoss:         0.01,
				TrailingStart:    0.015,
				TrailingGap:      0.004,
				EntryChange:      0.006,
				LookbackPoints:   20,
				Leverage:         5,
				CheckInterval:    5,
				BaseAsset:        "USDT",
				QuoteAssets:      []string{"BTC", "ETH", "BNB"},
			},
			MarginConfig: &MarginConfig{
				USDTPositionSize: 25,
				MaxPositions:     3,
				StopLoss:         0.01,
				TrailingStart:    0.015,
				TrailingGap:      0.004,
				EntryChange:      0.006,
				LookbackPoints:   20,
				CheckInterval:    5,
				BaseAsset:        "USDT",
				QuoteAssets:      []string{"BTC", "ETH", "BNB"},
				MarginType:       "ISOLATED",
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

	normalTargetProfit, err := strconv.ParseFloat(os.Getenv("TRADING_PROFIT_TARGET"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing TRADING_PROFIT_TARGET from .env file, using default 0.01 (1%)")
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

	// Parse futures trading configs
	futuresPositionSize, err := strconv.ParseFloat(os.Getenv("FUTURES_USDT_POSITION_SIZE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_USDT_POSITION_SIZE from .env file, using default 25 USDT")
		futuresPositionSize = 25
	}

	futuresMaxPositions, err := strconv.Atoi(os.Getenv("FUTURES_MAX_POSITIONS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_MAX_POSITIONS from .env file, using default 3")
		futuresMaxPositions = 3
	}

	futuresStopLoss, err := strconv.ParseFloat(os.Getenv("FUTURES_STOP_LOSS"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_STOP_LOSS from .env file, using default 0.01 (1%)")
		futuresStopLoss = 0.01
	}

	futuresTrailingStart, err := strconv.ParseFloat(os.Getenv("FUTURES_TRAILING_START"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_TRAILING_START from .env file, using default 0.015 (1.5%)")
		futuresTrailingStart = 0.015
	}

	futuresTrailingGap, err := strconv.ParseFloat(os.Getenv("FUTURES_TRAILING_GAP"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_TRAILING_GAP from .env file, using default 0.004 (0.4%)")
		futuresTrailingGap = 0.004
	}

	futuresEntryChange, err := strconv.ParseFloat(os.Getenv("FUTURES_ENTRY_CHANGE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_ENTRY_CHANGE from .env file, using default 0.006 (0.6%)")
		futuresEntryChange = 0.006
	}

	futuresLookbackPoints, err := strconv.Atoi(os.Getenv("FUTURES_LOOKBACK_POINTS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_LOOKBACK_POINTS from .env file, using default 20")
		futuresLookbackPoints = 20
	}

	futuresLeverage, err := strconv.Atoi(os.Getenv("FUTURES_LEVERAGE"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_LEVERAGE from .env file, using default 5")
		futuresLeverage = 5
	}

	futuresCheckInterval, err := strconv.Atoi(os.Getenv("FUTURES_CHECK_INTERVAL"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing FUTURES_CHECK_INTERVAL from .env file, using default 5 seconds")
		futuresCheckInterval = 5
	}

	futuresBaseAsset := os.Getenv("FUTURES_BASE_ASSET")
	if futuresBaseAsset == "" {
		futuresBaseAsset = "USDT"
	}

	futuresQuoteAssets := strings.Split(os.Getenv("FUTURES_QUOTE_ASSETS"), ",")
	if len(futuresQuoteAssets) == 1 && futuresQuoteAssets[0] == "" {
		futuresQuoteAssets = []string{"BTC", "ETH", "BNB"}
	}

	// Parse margin trading configs
	marginPositionSize, err := strconv.ParseFloat(os.Getenv("MARGIN_USDT_POSITION_SIZE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_USDT_POSITION_SIZE from .env file, using default 25 USDT")
		marginPositionSize = 25
	}

	marginMaxPositions, err := strconv.Atoi(os.Getenv("MARGIN_MAX_POSITIONS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_MAX_POSITIONS from .env file, using default 3")
		marginMaxPositions = 3
	}

	marginStopLoss, err := strconv.ParseFloat(os.Getenv("MARGIN_STOP_LOSS"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_STOP_LOSS from .env file, using default 0.01 (1%)")
		marginStopLoss = 0.01
	}

	marginTrailingStart, err := strconv.ParseFloat(os.Getenv("MARGIN_TRAILING_START"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_TRAILING_START from .env file, using default 0.015 (1.5%)")
		marginTrailingStart = 0.015
	}

	marginTrailingGap, err := strconv.ParseFloat(os.Getenv("MARGIN_TRAILING_GAP"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_TRAILING_GAP from .env file, using default 0.004 (0.4%)")
		marginTrailingGap = 0.004
	}

	marginEntryChange, err := strconv.ParseFloat(os.Getenv("MARGIN_ENTRY_CHANGE"), 64)
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_ENTRY_CHANGE from .env file, using default 0.006 (0.6%)")
		marginEntryChange = 0.006
	}

	marginLookbackPoints, err := strconv.Atoi(os.Getenv("MARGIN_LOOKBACK_POINTS"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_LOOKBACK_POINTS from .env file, using default 20")
		marginLookbackPoints = 20
	}

	marginCheckInterval, err := strconv.Atoi(os.Getenv("MARGIN_CHECK_INTERVAL"))
	if err != nil {
		log.Warn().Err(err).Msg("Error parsing MARGIN_CHECK_INTERVAL from .env file, using default 5 seconds")
		marginCheckInterval = 5
	}

	marginBaseAsset := os.Getenv("MARGIN_BASE_ASSET")
	if marginBaseAsset == "" {
		marginBaseAsset = "USDT"
	}

	marginQuoteAssets := strings.Split(os.Getenv("MARGIN_QUOTE_ASSETS"), ",")
	if len(marginQuoteAssets) == 1 && marginQuoteAssets[0] == "" {
		marginQuoteAssets = []string{"BTC", "ETH", "BNB"}
	}

	marginType := os.Getenv("MARGIN_TYPE")
	if marginType == "" {
		marginType = "ISOLATED"
	}
	if marginType != "ISOLATED" && marginType != "CROSS" {
		log.Warn().Msg("Invalid MARGIN_TYPE, must be ISOLATED or CROSS. Using default ISOLATED")
		marginType = "ISOLATED"
	}

	return &Config{
		GeneralConfig: &GeneralConfig{
			SimulationMode: os.Getenv("SIMULATION_MODE") == "true",
			APIKey:         os.Getenv("BINANCE_API_KEY"),
			APISecret:      os.Getenv("BINANCE_SECRET_KEY"),
			FeeRate:        fee,
			DebugMode:      os.Getenv("DEBUG_MODE") == "true",
			LogToConsole:   os.Getenv("LOG_TO_CONSOLE") == "true",
			TraceLogging:   os.Getenv("TRACE_LOGGING") == "true",
			VerboseLogging: os.Getenv("VERBOSE_LOGGING") == "true",
			WdEnabled:      os.Getenv("WD_ENABLED") == "true",
			TradingMode:    os.Getenv("TRADING_MODE"),
			Futures:        os.Getenv("FUTURES") == "true",
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
		NormalConfig: &TradingConfig{
			USDCAmount:   normalUSDCAmount,
			MaxPositions: normalMaxPositions,
			StopLoss:     normalStopLoss,
			MinProfit:    normalMinProfit,
			MaxHoldTime:  normalMaxHoldTime,
			TargetProfit: normalTargetProfit,
			BaseAsset:    os.Getenv("TRADING_BASE_ASSET"),
			QuoteAssets:  strings.Split(os.Getenv("TRADING_QUOTE_ASSETS"), ","),
		},
		FuturesConfig: &FuturesConfig{
			USDTPositionSize: futuresPositionSize,
			MaxPositions:     futuresMaxPositions,
			StopLoss:         futuresStopLoss,
			TrailingStart:    futuresTrailingStart,
			TrailingGap:      futuresTrailingGap,
			EntryChange:      futuresEntryChange,
			LookbackPoints:   futuresLookbackPoints,
			Leverage:         futuresLeverage,
			CheckInterval:    futuresCheckInterval,
			BaseAsset:        futuresBaseAsset,
			QuoteAssets:      futuresQuoteAssets,
		},
		MarginConfig: &MarginConfig{
			USDTPositionSize: marginPositionSize,
			MaxPositions:     marginMaxPositions,
			StopLoss:         marginStopLoss,
			TrailingStart:    marginTrailingStart,
			TrailingGap:      marginTrailingGap,
			EntryChange:      marginEntryChange,
			LookbackPoints:   marginLookbackPoints,
			CheckInterval:    marginCheckInterval,
			BaseAsset:        marginBaseAsset,
			QuoteAssets:      marginQuoteAssets,
			MarginType:       marginType,
		},
	}
}
