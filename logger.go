package main

import (
	"arbtribot/logger"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func DebugFmt(format string, args ...any) {
	if cfg.GeneralConfig.DebugMode {
		log.Debug().Caller(1).Msgf(format, args...)
	}
}

func SetUpLogger() {
	var writers []io.Writer

	if cfg.GeneralConfig.DebugMode || cfg.GeneralConfig.LogToConsole {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.DateTime,
			FormatLevel: func(i any) string {
				return outputLevelColor(i.(string), fmt.Sprintf("[%s]", strings.ToUpper(i.(string))))
			},
		})
	}

	writers = append(writers, logger.NewRollingFile("arbtribot"))
	mw := io.MultiWriter(writers...)

	log.Logger = log.Output(mw).With().Logger()
	log.Info().Msg("Logger initialized")
}

func outputLevelColor(level string, msg string) string {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return logger.Dim(msg) + logger.Reset()
	case "INFO":
		return logger.Blue(msg) + logger.Reset()
	case "WARN":
		return logger.Yellow(msg) + logger.Reset()
	case "ERROR":
		return logger.Red(msg) + logger.Reset()
	case "TRACE":
		return logger.BgBrightBlack(logger.Black(msg)) + logger.Reset()
	default:
		return msg
	}
}
