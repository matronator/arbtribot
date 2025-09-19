# Grid Trading Bot

This document describes the new grid trading functionality added to the arbitrage bot.

## Overview

The grid trading bot monitors cryptocurrency prices in real-time and executes trades based on price trends. It identifies upward price movements, buys the asset, and sells when a profit target is reached or stop loss is triggered.

## Features

- **Real-time Price Monitoring**: Tracks prices of multiple cryptocurrency pairs
- **Trend Detection**: Identifies upward price movements using configurable parameters
- **Position Management**: Manages multiple concurrent positions with profit/loss targets
- **Risk Management**: Implements stop-loss and maximum hold time limits
- **Simulation Mode**: Safe testing without real trades

## Configuration

The grid trading bot can be configured using environment variables in your `.env` file:

### Trading Mode
```
TRADING_MODE=grid  # Use "grid" for grid trading, "triangle" for arbitrage
```

### Grid Trading Parameters
```
GRID_MIN_PRICE_CHANGE=0.005    # Minimum price change to consider a trend (0.5%)
GRID_PROFIT_TARGET=0.02        # Target profit percentage (2%)
GRID_STOP_LOSS=0.01            # Stop loss percentage (1%)
GRID_MAX_HOLD_TIME=30          # Maximum hold time in minutes
GRID_MAX_POSITIONS=5           # Maximum number of concurrent positions
GRID_CHECK_INTERVAL=10         # Price check interval in seconds
```

## How It Works

1. **Price Monitoring**: The bot continuously monitors prices of USDC trading pairs
2. **Trend Analysis**: When a price shows an upward trend (configurable minimum change), it opens a position
3. **Position Management**: Each position has:
   - Entry price and quantity
   - Profit target (e.g., 2% gain)
   - Stop loss (e.g., 1% loss)
   - Maximum hold time (e.g., 30 minutes)
4. **Exit Conditions**: Positions are closed when:
   - Profit target is reached
   - Stop loss is triggered
   - Maximum hold time is exceeded

## Usage

1. Set `TRADING_MODE=grid` in your `.env` file
2. Configure grid trading parameters as needed
3. Run the bot: `go run main.go`

The bot will automatically:
- Start monitoring prices
- Open positions when trends are detected
- Close positions based on profit/loss targets
- Log all trading activity

## Safety Features

- **Simulation Mode**: By default, the bot runs in simulation mode (no real trades)
- **Position Limits**: Maximum number of concurrent positions prevents over-exposure
- **Stop Loss**: Automatic loss cutting to limit downside risk
- **Time Limits**: Maximum hold time prevents positions from being held indefinitely

## Logging

The bot provides detailed logging including:
- Position openings and closings
- Profit/loss calculations
- Price trend analysis
- Error handling and recovery

## Example Output

```
[INFO] Starting Grid Trading Bot...
[INFO] Grid Config: MinChange=0.50%, ProfitTarget=2.00%, StopLoss=1.00%, MaxPositions=5
[INFO] Monitoring 20 symbols for trading opportunities
[INFO] BUY Opened position: BTCUSDC at 45000.000000 USDC (Qty: 0.000333)
[INFO] SELL Closed position: BTCUSDC at 45900.000000 USDC (P/L: +2.00% / +0.30 USDC) - Reason: PROFIT
```

## Switching Between Modes

To switch between grid trading and triangle arbitrage:

1. **Grid Trading**: Set `TRADING_MODE=grid`
2. **Triangle Arbitrage**: Set `TRADING_MODE=triangle` (or leave empty)

Both modes can run in simulation or live trading mode based on the `SIMULATION_MODE` setting.
