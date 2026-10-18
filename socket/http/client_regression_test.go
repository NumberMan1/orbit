package http

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type customTransport struct{ calls int }

func (t *customTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header), Request: r}, nil
}

func TestClientWithCustomDefaultTransport(t *testing.T) {
	old := http.DefaultTransport
	transport := &customTransport{}
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = old }()
	if _, err := NewClient(time.Second); err == nil {
		t.Fatal("custom default transport needs explicit ownership")
	}
	client, err := NewClientWithTransport(time.Second, transport)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Get("http://example.invalid")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if transport.calls != 1 || client.Transport != transport || client.Timeout != time.Second {
		t.Fatal("injected transport not used")
	}
	if _, err := NewClientWithTransport(time.Second, nil); err == nil {
		t.Fatal("accepted nil transport")
	}
	if _, err := NewClientWithTransport(time.Second, (*customTransport)(nil)); err == nil {
		t.Fatal("accepted typed nil transport")
	}
}
