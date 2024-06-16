package main

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/uxrust/clientx"
)

type CatFactAPI struct {
	*clientx.API
}

func New(api *clientx.API) *CatFactAPI {
	return &CatFactAPI{
		API: api,
	}
}

type (
	EmptyRequest struct{}
	Fact         struct {
		Fact   string `json:"fact"`
		Length int    `json:"length"`
	}
)

func (api *CatFactAPI) Get(ctx context.Context, opts ...clientx.RequestOption) (*Fact, error) {
	return clientx.NewRequestBuilder[EmptyRequest, Fact](api.API).
		Get("/fact", opts...).
		DoWithDecode(ctx)
}

func main() {
	api := New(
		clientx.NewAPI(
			clientx.WithBaseURL("https://catfact.ninja"),
			clientx.WithRateLimit(1, 3, time.Second),
			clientx.WithRetry(
				3,
				time.Second,
				time.Second*2,
				clientx.ExponentalBackoff,
				func(resp *http.Response, err error) bool {
					return resp != nil && resp.StatusCode == http.StatusTooManyRequests
				},
			),
			clientx.WithCircuitBreaker(
				"main-breaker",
				10,
				3,
			),
		),
	)

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
			defer cancel()
			resp, err := api.Get(ctx)
			if err != nil {
				fmt.Printf("Goroutine %d, Error: %s\n", i+1, err.Error())
				return
			}
			fmt.Printf("Goroutine %d, Fact (len=%d): %s\n", i+1, resp.Length, resp.Fact)
		}(i)
	}
	wg.Wait()
}
