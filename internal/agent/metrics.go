package agent

type Metrics struct {
	Gauges   map[string]float64
	Counters map[string]int64
}

func NewMetrics() *Metrics {
	return &Metrics{
		Gauges:   make(map[string]float64),
		Counters: make(map[string]int64),
	}
}
