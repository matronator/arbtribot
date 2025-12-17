package logger

import (
	"strconv"

	"github.com/quagmt/udecimal"
)

func ColorizeReason(reason string) string {
	switch reason {
	case "STOP_LOSS":
		return Red(reason)
	case "TAKE_PROFIT":
	case "PROFIT_TARGET":
	case "TRAILING_EXIT":
		return Green(reason)
	case "MAX_HOLD_TIME":
		return Yellow(reason)
	case "TIMEOUT":
		return Blue(reason)
	}
	return reason
}

func ColorizeNumber(i float64, min *float64, max *float64) string {
	var minVal, maxVal float64

	if min == nil {
		minVal = 0
	} else {
		minVal = *min
	}
	if max == nil {
		maxVal = 100
	} else {
		maxVal = *max
	}

	// Handle edge case where min == max
	if maxVal == minVal {
		return Yellow(strconv.FormatFloat(i, 'f', 0, 64)) // Default to yellow if range is zero
	}

	percentage := (i - minVal) / (maxVal - minVal) * 100

	switch {
	case percentage > 66 && percentage <= 100:
		return Red(strconv.FormatFloat(i, 'f', 0, 64))
	case percentage >= 33 && percentage <= 66:
		return Yellow(strconv.FormatFloat(i, 'f', 0, 64))
	case percentage >= 0 && percentage <= 33:
		return Green(strconv.FormatFloat(i, 'f', 0, 64))
	default:
		return Dim(strconv.FormatFloat(i, 'f', 0, 64))
	}
}

func ColorizePnl(pnl udecimal.Decimal) string {
	if pnl.Cmp(udecimal.Zero) > 0 {
		return Green(pnl.StringFixed(4))
	} else if pnl.Cmp(udecimal.Zero) < 0 {
		return Red(pnl.StringFixed(4))
	}
	return Yellow(pnl.StringFixed(4))
}

func ColorizeSide(side string) string {
	switch side {
	case "LONG":
	case "BUY":
		return Green(side)
	case "SHORT":
	case "SELL":
		return Red(side)
	default:
		return Yellow(side)
	}
	return side
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

func Black(msg string) string {
	return "\033[30m" + msg + "\033[39m" // Black
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
