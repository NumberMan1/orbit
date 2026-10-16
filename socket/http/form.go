package http

import (
	"context"
	"encoding/json/v2"
	"errors"
	"io"
	"mime"
	stdhttp "net/http"
	"net/url"
	"strings"
)

// DecodeForm returns body fields only (not URL query parameters), preserves
// repeated values, and rejects malformed escaping and oversized bodies.
// Multipart uploads use net/http's MultipartReader and application file limits.
func DecodeForm(w stdhttp.ResponseWriter, req *stdhttp.Request, limit int64) (url.Values, error) {
	if limit <= 0 {
		return nil, errors.New("http: positive form limit required")
	}
	kind, _, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	if err != nil || kind != "application/x-www-form-urlencoded" {
		return nil, errors.New("http: URL-encoded form required")
	}
	data, err := io.ReadAll(stdhttp.MaxBytesReader(w, req.Body, limit))
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(data))
	if err != nil {
		return nil, err
	}
	return values, nil
}

// WriteJSON marshals BEFORE committing headers, preserving status codes and
// returning serialization/write errors. It does not write an implicit HTTP 200.
func WriteJSON(w stdhttp.ResponseWriter, status int, value any) error {
	if status < 200 || status > 599 || status == stdhttp.StatusNoContent || status == stdhttp.StatusResetContent || status == stdhttp.StatusNotModified {
		return errors.New("http: final status allowing a response body required")
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	n, err := w.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	return err
}

// DoForm sends explicit url.Values and decodes an optional JSON response.
func DoForm(ctx context.Context, client *stdhttp.Client, method, address string, values url.Values, response any, maxResponse int64) error {
	if client == nil || maxResponse <= 0 || maxResponse == 1<<63-1 {
		return errors.New("http: client and valid response limit required")
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, address, strings.NewReader(values.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return doResponse(client, req, response, maxResponse)
}
