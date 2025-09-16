package logger

import (
	"fmt"
	"io"
	"regexp"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

type ANSIStripper struct {
	writer io.Writer
}

func Debug(format string, args ...any) {
	log.Debug().Caller(1).Msgf(format, args...)
}

func DebugFmt(format string, args ...any) {
	log.Debug().Caller(1).Msgf(format, args...)
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
