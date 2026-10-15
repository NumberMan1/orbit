package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPJSONAndLifecycle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("GET /large", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", 100))) })
	mux.HandleFunc("GET /duplicate", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","status":"bad"}`))
	})
	s, err := NewServer("127.0.0.1:0", mux, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer s.Stop(context.Background())
	client, _ := NewClient(time.Second)
	defer client.CloseIdleConnections()
	var response struct {
		Status string `json:"status"`
	}
	base := "http://" + s.Addr().String()
	if err := DoJSON(t.Context(), client, "GET", base+"/healthz", nil, &response, 1024); err != nil || response.Status != "ok" {
		t.Fatalf("%+v %v", response, err)
	}
	if err := DoJSON(t.Context(), client, "GET", base+"/missing", nil, nil, 1024); err == nil {
		t.Fatal("404 ignored")
	}
	if err := DoJSON(t.Context(), client, "GET", base+"/large", nil, nil, 10); err == nil {
		t.Fatal("size ignored")
	}
	if err := DoJSON(t.Context(), client, "GET", base+"/duplicate", nil, &response, 1024); err == nil {
		t.Fatal("duplicate response fields accepted")
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.Stop(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestStrictRequest(t *testing.T) {
	for _, body := range []string{"", `{"extra":1}`, `{"Value":1}`, `{"value":1,"value":2}`, `{"value":1,"\u0076alue":2}`, `{"value":1}{"value":2}`, `{"value":1}` + strings.Repeat(" ", 32), strings.Repeat("x", 100)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		var v struct {
			Value int `json:"value"`
		}
		if err := DecodeJSON(httptest.NewRecorder(), r, &v, 32); err == nil {
			t.Fatalf("accepted %s", body)
		}
	}
}

func TestJSONV2RequestBoundaryAndReadErrors(t *testing.T) {
	body := " {\"value\":\"ok\"}\n"
	var dst struct {
		Value string `json:"value"`
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader(body))
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst, int64(len(body))); err != nil || dst.Value != "ok" {
		t.Fatalf("exact limit with whitespace: %+v %v", dst, err)
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader("{\"value\":\"\xff\"}"))
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst, 64); err == nil {
		t.Fatal("invalid UTF-8 accepted")
	}
	r = httptest.NewRequest("POST", "/", strings.NewReader(body))
	var tooLarge *http.MaxBytesError
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst, int64(len(body)-1)); !errors.As(err, &tooLarge) {
		t.Fatalf("body limit error lost: %v", err)
	}
	failure := errors.New("request read failed")
	r = httptest.NewRequest("POST", "/", nil)
	r.Body = io.NopCloser(failedReader{err: failure})
	if err := DecodeJSON(httptest.NewRecorder(), r, &dst, 64); !errors.Is(err, failure) {
		t.Fatalf("read error lost: %v", err)
	}
}

type failedReader struct{ err error }

func (r failedReader) Read([]byte) (int, error) { return 0, r.err }
