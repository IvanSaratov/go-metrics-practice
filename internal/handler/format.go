package handler

import "strconv"

func formatGauge(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func formatCounter(value int64) string {
	return strconv.FormatInt(value, 10)
}
