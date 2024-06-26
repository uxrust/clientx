package clientx

import (
	"bytes"
	"context"
	"fmt"
	"github.com/pkg/errors"
	"github.com/sony/gobreaker/v2"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"
)

type client[Req any, Resp any] struct {
	api           *API
	afterResponse []func(resp *http.Response, data *Resp) error
}

func (c *client[Req, Resp]) do(ctx context.Context, req *RequestBuilder[Req, Resp], decode bool, enc EncoderDecoder) (*http.Response, *Resp, error) {
	// Wait for ratelimits. It is a blocking call.
	if err := c.api.limiter.Wait(ctx); err != nil {
		return nil, nil, err
	}

	// Create HTTP request and apply beforeResponse chain
	httpReq, err := c.makeRequest(ctx, req, enc)
	if err != nil {
		return nil, nil, err
	}

	resp, err := c.performRequest(ctx, httpReq, req)
	if err != nil {
		return nil, nil, err
	}
	r, err := responseReader(resp)
	if err != nil {
		return nil, nil, err
	}
	defer r.Close()

	if req.errDecodeFn != nil {
		ok, err := req.errDecodeFn(resp)
		if ok {
			return resp, nil, err
		}
	}

	var data Resp
	if decode && enc != nil {
		if err := decodeResponse(enc, r, &data); err != nil {
			return nil, nil, err
		}
	}

	for _, after := range c.afterResponse {
		if err := after(resp, &data); err != nil {

			return nil, nil, errors.Wrap(err, "after response exec failed")
		}
	}

	return resp, &data, nil
}

func (c *client[Req, Resp]) makeRequest(ctx context.Context, req *RequestBuilder[Req, Resp], enc EncoderDecoder) (*http.Request, error) {
	u, err := c.buildRequestURL(req.resourcePath)
	if err != nil {
		return nil, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, req.method, u.String(), nil)
	if err != nil {
		return nil, err
	}
	// If method is not GET, try to set payload body
	if req.method != http.MethodGet && req.body != nil && enc != nil {
		httpReq.Body, err = req.encodeRequestPayload(enc)
		if err != nil {
			return nil, err
		}
	}
	if len(c.api.options.Headers) != 0 {
		httpReq.Header = c.api.options.Headers
	}

	// Apply options to request
	for _, opt := range req.requestOptions {
		if err := opt(httpReq); err != nil {
			return nil, err
		}
	}

	return httpReq, nil
}

func (c *client[Req, Resp]) performRequest(ctx context.Context, httpReq *http.Request, req *RequestBuilder[Req, Resp]) (*http.Response, error) {
	do := func(c *client[Req, Resp], req *http.Request, reuse bool) (*http.Response, error) {
		if reuse && req.Body != nil {
			// Issue https://github.com/golang/go/issues/36095
			var b bytes.Buffer
			b.ReadFrom(req.Body)
			req.Body = io.NopCloser(&b)

			cloneReq := req.Clone(ctx)
			cloneReq.Body = io.NopCloser(bytes.NewReader(b.Bytes()))
			req = cloneReq
		}

		var resp *http.Response
		var err error

		if c.api.breaker == nil || c.api.breaker.Breaker == nil {
			resp, err = c.api.httpClient.Do(req)
			if err != nil {
				return nil, err
			}
		} else {
			resp, err = c.api.breaker.Breaker.Execute(func() (*http.Response, error) {
				resp, err := c.api.httpClient.Do(req)
				if err != nil {
					return nil, err
				}

				return resp, nil
			})
		}

		if err != nil {
			if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
				// todo add metrics of c-breaker
				return nil, errors.Wrap(err, fmt.Sprintf("circuit-breaker has worked"))
			}

			return nil, err
		}

		if c.api.options.Debug {
			b, err := httputil.DumpResponse(resp, true)
			if err != nil {
				return nil, err
			}
			_, _ = fmt.Fprintf(os.Stdout, "RESPONSE:\n%s\n", string(b))
		}
		return resp, nil
	}
	if c.api.retry == nil {
		// Do single request without using backoff retry mechanism
		return do(c, httpReq, false)
	}

	for {
		resp, err := do(c, httpReq, true)

		var isMatchedCond bool
		for _, cond := range c.api.options.Retry.Conditions {
			if ok := cond(resp, err); ok {
				isMatchedCond = true
				break
			}
		}
		if isMatchedCond {
			// Get next duration interval, sleep and make another request
			// till nextDuration != stopBackoff
			nextDuration := c.api.retry.Next()
			if nextDuration == stopBackoff {
				c.api.retry.Reset()
				return resp, err
			}
			time.Sleep(nextDuration)
			continue
		}

		// Break retries mechanism if conditions weren't matched
		return resp, err
	}
}

func (c *client[Req, Resp]) buildRequestURL(resource string) (*url.URL, error) {
	u, err := url.Parse(c.api.options.BaseURL)
	if err != nil {
		return nil, err
	}
	u.Path = resource
	return u, nil
}
