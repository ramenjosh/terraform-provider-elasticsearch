package es

import (
	"net/http"
)

type withHeader struct {
	http.Header
	hostOverride string
	rt           http.RoundTripper
}

func WithHeader(rt http.RoundTripper, hostOverride string) withHeader {
	if rt == nil {
		rt = http.DefaultTransport
	}

	return withHeader{Header: make(http.Header), rt: rt, hostOverride: hostOverride}
}

func (h withHeader) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range h.Header {
		req.Header[k] = v
	}
	if h.hostOverride != "" {
		req.Host = h.hostOverride
	}

	return h.rt.RoundTrip(req)
}
