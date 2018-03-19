# Metricsgen

[![GoDoc](https://godoc.org/github.com/gojuno/metricsgen?status.svg)](http://godoc.org/github.com/gojuno/metricsgen)

Metricsgen generates interface implementation that measures execution time of all methods using prometheus SDK.

## Installation

```
go get -u github.com/gojuno/metricsgen
```

## Usage

Imagine you have the following interface:


```go
type Example interface {
	Do(a, b string) error
	Another(c string)
}
```

Here is how to generate metrics decorator for this interface:
```
metricsgen -i github.com/gojuno/metricsgen/tests.Example -o ./tests/
```

The result will be:
```go
package tests

/*
DO NOT EDIT!
This code was generated automatically using github.com/gojuno/metricsgen v1.0
The original interface "Example" can be found in github.com/gojuno/metricsgen/tests
*/
import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type ExampleMetrics struct {
	next     Example
	summary  *prometheus.SummaryVec
	instance string
}

func NewExampleMetricsWithSummary(next Example, instance string, sv *prometheus.SummaryVec) *ExampleMetrics {
	return &ExampleMetrics{
		next:     next,
		summary:  sv,
		instance: instance,
	}
}

func NewExampleMetrics(next Example, metricName, instance string) *ExampleMetrics {
	sv := prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name: metricName,
			Help: metricName,
		},
		[]string{"instance", "method"},
	)

	prometheus.MustRegister(sv)

	return NewExampleMetricsWithSummary(next, instance, sv)
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
	m.summary.WithLabelValues(m.instance, method).Observe(duration.Seconds())
}
```

### Metrics

Decorator creates prometheus summary vector with two labels:

- `instance`. it's used to separate different instances of single interface to avoid metric name collisions. 
- `method`. Interface method name.

### Constructors
Decorator provides two constructors to being used. 

1. `NewExampleMetrics` creates summary vector internally using passed `metricName`.

```go
ex := NewExampleMetrics(next, "example_metric_name", "instance_1")
```

2. `NewExampleMetricsWithSummary` uses passed summary vector. It's useful when you want to use several instances of interface.

```go
sv := NewExampleMetricsSummary("example_metric_name")
ex1 := NewExampleMetricsWithSummary(next, sv, "instance_1")
ex2 := NewExampleMetricsWithSummary(next, sv, "instance_2")
```
