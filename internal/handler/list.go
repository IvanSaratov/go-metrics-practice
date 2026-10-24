package handler

import (
	"html/template"
	"net/http"
	"sort"

	"github.com/IvanSaratov/go-metrics-practice/internal/repository"
)

// ListHandler показывает все метрики на HTML-странице.
type ListHandler struct {
	storage repository.Storage
}

type metricView struct {
	Name  string
	Value string
}

type listView struct {
	Gauges   []metricView
	Counters []metricView
}

var listTemplate = template.Must(template.New("metrics").Parse(`<!doctype html>
<html>
<head>
	<meta charset="utf-8">
	<title>Metrics</title>
</head>
<body>
	<h1>Metrics</h1>
	<h2>Gauges</h2>
	<ul>
	{{range .Gauges}}
		<li>{{.Name}}: {{.Value}}</li>
	{{end}}
	</ul>
	<h2>Counters</h2>
	<ul>
	{{range .Counters}}
		<li>{{.Name}}: {{.Value}}</li>
	{{end}}
	</ul>
</body>
</html>`))

// NewListHandler создаёт обработчик списка метрик из storage.
func NewListHandler(storage repository.Storage) *ListHandler {
	return &ListHandler{
		storage: storage,
	}
}

// ServeHTTP возвращает HTML-страницу с метриками по GET /.
// Gauge и counter выведены отдельно, в каждой группе метрики отсортированы по имени.
func (h *ListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	gauges, err := h.storage.GetAllGauges(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	counters, err := h.storage.GetAllCounters(r.Context())
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	view := listView{
		Gauges:   make([]metricView, 0),
		Counters: make([]metricView, 0),
	}

	for name, value := range gauges {
		view.Gauges = append(view.Gauges, metricView{
			Name:  name,
			Value: formatGauge(value),
		})
	}

	for name, value := range counters {
		view.Counters = append(view.Counters, metricView{
			Name:  name,
			Value: formatCounter(value),
		})
	}

	sortMetrics(view.Gauges)
	sortMetrics(view.Counters)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = listTemplate.Execute(w, view)
}

func sortMetrics(metrics []metricView) {
	sort.Slice(metrics, func(i, j int) bool {
		return metrics[i].Name < metrics[j].Name
	})
}
