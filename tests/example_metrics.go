package tests

/*
DO NOT EDIT!
This code was generated automatically using github.com/gojuno/metricsgen v1.2
The original interface "Example" can be found in github.com/gojuno/metricsgen/tests
*/
import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type ExampleMetrics struct {
	next    Example
	summary *prometheus.SummaryVec
	name    string
}

func NewExampleMetricsSummary(metricName string) *prometheus.SummaryVec {
	sv := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: metricName,
			Help: metricName,
		},
		[]string{"name", "method"},
	)

	prometheus.MustRegister(sv)

	return sv
}

func NewExampleMetricsWithSummary(next Example, name string, sv *prometheus.SummaryVec) *ExampleMetrics {
	return &ExampleMetrics{
		next:    next,
		summary: sv,
		name:    name,
	}
}

func (m *ExampleMetrics) Another(p string) {
	defer m.observe("Another", time.Now())

	m.next.Another(p)
}

func (m *ExampleMetrics) Do(p string, p1 string) (r error) {
	defer m.observe("Do", time.Now())

	return m.next.Do(p, p1)
}

func (m *ExampleMetrics) observe(method string, startedAt time.Time) {
	duration := time.Since(startedAt)
	m.summary.WithLabelValues(m.name, method).Observe(duration.Seconds())
}
