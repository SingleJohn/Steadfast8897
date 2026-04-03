package utils

const (
	TicksPerSecond = 10_000_000
	TicksPerMs     = 10_000
)

func SecondsToTicks(seconds float64) int64 {
	return int64(seconds*TicksPerSecond + 0.5)
}

func TicksToSeconds(ticks int64) float64 {
	return float64(ticks) / TicksPerSecond
}

func MsToTicks(ms int64) int64 {
	return ms * TicksPerMs
}

func TicksToMs(ticks int64) int64 {
	return ticks / TicksPerMs
}
