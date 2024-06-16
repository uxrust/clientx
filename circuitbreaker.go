package clientx

import (
	"github.com/sony/gobreaker/v2"
	"net/http"
	"time"
)

type CircuitBreaker struct {
	Breaker *gobreaker.CircuitBreaker[*http.Response]
}

func newCircuitBreaker(cbOption *OptionCircuitBreaker) *CircuitBreaker {
	var st gobreaker.Settings
	st.Name = cbOption.Name
	st.Timeout = time.Duration(cbOption.BreakerTimeOutInSeconds) * time.Second
	st.ReadyToTrip = func(counts gobreaker.Counts) bool {
		// failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
		// return counts.Requests >= 3 && failureRatio >= 0.6

		return counts.ConsecutiveFailures >= cbOption.ConsecutiveFailuresLimit
	}

	cb := CircuitBreaker{}
	cb.Breaker = gobreaker.NewCircuitBreaker[*http.Response](st)

	return &cb
}
