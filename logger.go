package main

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

func InfoFmt(format string, args ...any) {
	log.Info().Caller(1).Msgf(format, args...)
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

func DebugFmt(format string, args ...any) {
	if cfg.DebugMode {
		log.Debug().Caller(1).Msgf(format, args...)
	}
}

func SetUpLogger() {
	var writers []io.Writer

	if cfg.DebugMode {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.DateTime,
			FormatLevel: func(i any) string {
				return outputLevelColor(i.(string), fmt.Sprintf("[%s]", strings.ToUpper(i.(string))))
			},
		})
	}

	writers = append(writers, newRollingFile())
	mw := io.MultiWriter(writers...)

	log.Logger = log.Output(mw).With().Logger()
	log.Info().Msg("Logger initialized")
}

type ansiStripper struct {
	writer io.Writer
}

func (a *ansiStripper) Write(p []byte) (n int, err error) {
	ansiRegex := regexp.MustCompile(`\\u001b[[0-9]+?m`)
	cleaned := ansiRegex.ReplaceAll(p, []byte(""))
	_, err = a.writer.Write(cleaned)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func newRollingFile() io.Writer {
	rollingFile := &lumberjack.Logger{
		Filename:   "log/arbtribot.log",
		MaxSize:    5, // megabytes
		MaxBackups: 5,
		MaxAge:     30, // days
		Compress:   false,
		LocalTime:  true,
	}
	return &ansiStripper{writer: rollingFile}
}

func outputLevelColor(level string, msg string) string {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return Dim(msg) + Reset()
	case "INFO":
		return Blue(msg) + Reset()
	case "WARN":
		return Yellow(msg) + Reset()
	case "ERROR":
		return Red(msg) + Reset()
	default:
		return msg
	}
}

func Reset() string {
	return "\033[0m" // Reset all attributes
}

func Bold(msg string) string {
	return "\033[1m" + msg + "\033[21m" // Bold
}

func Dim(msg string) string {
	return "\033[2m" + msg + "\033[22m" // Dim
}

func Italic(msg string) string {
	return "\033[3m" + msg + "\033[23m" // Italic
}

func Underline(msg string) string {
	return "\033[4m" + msg + "\033[24m" // Underline
}

func Blink(msg string) string {
	return "\033[5m" + msg + "\033[25m" // Slow Blink
}

func Reverse(msg string) string {
	return "\033[7m" + msg + "\033[27m" // Reverse
}

func Red(msg string) string {
	return "\033[31m" + msg + "\033[39m" // Red
}

func Green(msg string) string {
	return "\033[32m" + msg + "\033[39m" // Green
}

func Yellow(msg string) string {
	return "\033[33m" + msg + "\033[39m" // Yellow
}

func Blue(msg string) string {
	return "\033[34m" + msg + "\033[39m" // Blue
}

func Magenta(msg string) string {
	return "\033[35m" + msg + "\033[39m" // Magenta
}

func Cyan(msg string) string {
	return "\033[36m" + msg + "\033[39m" // Cyan
}

func White(msg string) string {
	return "\033[37m" + msg + "\033[39m" // White
}

func BrightBlack(msg string) string {
	return "\033[90m" + msg + "\033[39m" // Bright Black
}

func BrightRed(msg string) string {
	return "\033[91m" + msg + "\033[39m" // Bright Red
}

func BrightGreen(msg string) string {
	return "\033[92m" + msg + "\033[39m" // Bright Green
}

func BrightYellow(msg string) string {
	return "\033[93m" + msg + "\033[39m" // Bright Yellow
}

func BrightBlue(msg string) string {
	return "\033[94m" + msg + "\033[39m" // Bright Blue
}

func BrightMagenta(msg string) string {
	return "\033[95m" + msg + "\033[39m" // Bright Magenta
}

func BrightCyan(msg string) string {
	return "\033[96m" + msg + "\033[39m" // Bright Cyan
}

func BrightWhite(msg string) string {
	return "\033[97m" + msg + "\033[39m" // Bright White
}

func BgRed(msg string) string {
	return "\033[41m" + msg + "\033[49m" // Background Red
}

func BgGreen(msg string) string {
	return "\033[42m" + msg + "\033[49m" // Background Green
}

func BgYellow(msg string) string {
	return "\033[43m" + msg + "\033[49m" // Background Yellow
}

func BgBlue(msg string) string {
	return "\033[44m" + msg + "\033[49m" // Background Blue
}

func BgMagenta(msg string) string {
	return "\033[45m" + msg + "\033[49m" // Background Magenta
}

func BgCyan(msg string) string {
	return "\033[46m" + msg + "\033[49m" // Background Cyan
}

func BgWhite(msg string) string {
	return "\033[47m" + msg + "\033[49m" // Background White
}

func BgBrightBlack(msg string) string {
	return "\033[100m" + msg + "\033[49m" // Background Bright Black
}

func BgBrightRed(msg string) string {
	return "\033[101m" + msg + "\033[49m" // Background Bright Red
}

func BgBrightGreen(msg string) string {
	return "\033[102m" + msg + "\033[49m" // Background Bright Green
}

func BgBrightYellow(msg string) string {
	return "\033[103m" + msg + "\033[49m" // Background Bright Yellow
}

func BgBrightBlue(msg string) string {
	return "\033[104m" + msg + "\033[49m" // Background Bright Blue
}

func BgBrightMagenta(msg string) string {
	return "\033[105m" + msg + "\033[49m" // Background Bright Magenta
}

func BgBrightCyan(msg string) string {
	return "\033[106m" + msg + "\033[49m" // Background Bright Cyan
}

func BgBrightWhite(msg string) string {
	return "\033[107m" + msg + "\033[49m" // Background Bright White
}
