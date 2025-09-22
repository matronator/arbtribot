package logger

import (
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

type ANSIStripper struct {
	writer io.Writer
}

func Debug(format string) {
	log.Debug().Caller(1).Msg(format)
}

func DebugFmt(format string, args ...any) {
	log.Debug().Caller(1).Msgf(format, args...)
}

func Trace(format string) {
	newLogger := log.With().Logger().Sample(&zerolog.LevelSampler{TraceSampler: newTraceSampler()})
	newLogger.Trace().Caller(1).Msg(format)
}

func TraceFmt(format string, args ...any) {
	newLogger := log.With().Logger().Sample(&zerolog.LevelSampler{TraceSampler: newTraceSampler()})
	newLogger.Trace().Caller(1).Msgf(format, args...)
}

func newTraceSampler() *zerolog.BurstSampler {
	return &zerolog.BurstSampler{
		Burst:       2,
		Period:      time.Second * 5,
		NextSampler: &zerolog.BasicSampler{N: 50},
	}
}

func Info(msg string) {
	log.Info().Caller(1).Msg(msg)
}

func InfoFmt(format string, args ...any) {
	log.Info().Caller(1).Msgf(format, args...)
}

func Warning(msg string) {
	log.Warn().Caller(1).Msg(msg)
}

func WarningFmt(format string, args ...any) {
	log.Warn().Caller(1).Msgf(format, args...)
}

func ErrorFmt(format string, args ...any) {
	log.Error().Stack().Caller(1).Msgf(format, args...)
}

func Error(err error) {
	log.Error().Stack().Caller(1).Err(err).Send()
}

func NewSimTradeWriter() zerolog.Logger {
	return zerolog.New(NewRollingFile("simulated-trades")).With().Timestamp().Logger()
}

func (a *ANSIStripper) Write(p []byte) (n int, err error) {
	ansiRegex := regexp.MustCompile(`\\u001b[[0-9]+?m`)
	cleaned := ansiRegex.ReplaceAll(p, []byte(""))
	_, err = a.writer.Write(cleaned)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func NewRollingFile(name string) io.Writer {
	rollingFile := &lumberjack.Logger{
		Filename:   fmt.Sprintf("log/%s.log", name),
		MaxSize:    10, // megabytes
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   false,
		LocalTime:  true,
	}
	return &ANSIStripper{writer: rollingFile}
}
