package main

import (
	"github.com/rs/zerolog/log"
)

func main() {
	cfg := LoadConfig()
	SetUpLogger(cfg.DebugMode)
	log.Info().
		Bool("SIMULATION_MODE", cfg.SimulationMode).
		Bool("DEBUG_MODE", cfg.DebugMode).
		Str("API_KEY", "***").
		Str("API_SECRET", "***").
		Float64("FEE_RATE", cfg.FeeRate).
		Msg("Bot started with config from .env file")
	if cfg.SimulationMode {
		Info("MODE: %s %s", Green("SIMULATION"), Italic(Dim("(no real trades will be placed, only logs)")))
	} else {
		Info("MODE: %s %s", Yellow("LIVE TRADING"), Italic(BgYellow("(real trades will be placed)")))
	}

	defer func() {
		Info("Bot is shutting down...")
		Info("See you next time! %s", Blue("Arbtribot ended..."))
	}()
}
