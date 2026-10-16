package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFormAndResponseStatus(t *testing.T) {
	values := url.Values{"tag": {"one", "two"}, "text": {"空 格&+"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got, err := DecodeForm(w, r, 256)
		if err != nil || !reflect.DeepEqual(got, values) {
			t.Error(got, err)
			http.Error(w, "bad form", 400)
			return
		}
		if err := WriteJSON(w, http.StatusCreated, map[string]bool{"ok": true}); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	client, _ := NewClient(time.Second)
	defer client.CloseIdleConnections()
	var response map[string]bool
	if err := DoForm(t.Context(), client, "PUT", server.URL+"?ignored=value", values, &response, 64); err != nil || !response["ok"] {
		t.Fatal(response, err)
	}
	w := httptest.NewRecorder()
	if err := WriteJSON(w, 201, make(chan int)); err == nil || w.Body.Len() != 0 || len(w.Header()) != 0 {
		t.Fatal("serialization committed headers", err)
	}
	if err := WriteJSON(w, 201, map[string]bool{"ok": true}); err != nil || w.Code != 201 {
		t.Fatal(w.Code, err)
	}
	for _, body := range []string{"bad=%ZZ", strings.Repeat("x", 65)} {
		r := httptest.NewRequest("POST", "/", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if _, err := DecodeForm(httptest.NewRecorder(), r, 64); err == nil {
			t.Fatal("invalid form accepted")
		}
	}
	r := httptest.NewRequest("POST", "/", strings.NewReader("a=1"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var limit *http.MaxBytesError
	if _, err := DecodeForm(httptest.NewRecorder(), r, 2); !errors.As(err, &limit) {
		t.Fatal(err)
	}
}
