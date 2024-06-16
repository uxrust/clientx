package clientx

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type RequestOption func(req *http.Request) error

// WithRequestQueryEncodableParams encodes query params by implementing ParamEncoder[T] interface,
// calls Encode(url.Values) functional to set query params.
func WithRequestQueryEncodableParams[T any](params ...ParamEncoder[T]) RequestOption {
	return func(req *http.Request) error {
		q := req.URL.Query()
		for _, param := range params {
			if err := param.Encode(q); err != nil {
				return fmt.Errorf("failed to encode query params: %w", err)
			}
		}
		req.URL.RawQuery = q.Encode()
		return nil
	}
}

func WithRequestForm(form url.Values) RequestOption {
	return func(req *http.Request) error {
		req.Body = io.NopCloser(strings.NewReader(form.Encode()))
		return nil
	}
}

func WithRequestHeaders(headers map[string][]string) RequestOption {
	return func(req *http.Request) error {
		req.Header = headers
		return nil
	}
}
