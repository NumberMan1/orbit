package http

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	stdhttp "net/http"
	"time"
)

// DecodeJSON enforces a byte limit and JSON v2's strict UTF-8 and name rules.
// It rejects unknown fields, duplicate names and trailing values.
func DecodeJSON(w stdhttp.ResponseWriter, req *stdhttp.Request, dst any, limit int64) error {
	if limit <= 0 {
		return errors.New("http: positive body limit required")
	}
	return json.UnmarshalRead(stdhttp.MaxBytesReader(w, req.Body, limit), dst, json.RejectUnknownMembers(true))
}

func NewClient(timeout time.Duration) (*stdhttp.Client, error) {
	if timeout <= 0 {
		return nil, errors.New("http: positive client timeout required")
	}
	return &stdhttp.Client{Timeout: timeout, Transport: stdhttp.DefaultTransport.(*stdhttp.Transport).Clone()}, nil
}

// DoJSON supports all HTTP methods and returns non-2xx statuses as errors.
// The caller owns client and should CloseIdleConnections on shutdown.
func DoJSON(ctx context.Context, client *stdhttp.Client, method, url string, request, response any, maxResponse int64) error {
	if client == nil || maxResponse <= 0 || maxResponse == 1<<63-1 {
		return errors.New("http: client and valid response limit required")
	}
	var body io.Reader
	if request != nil {
		b, err := json.Marshal(request)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := stdhttp.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return err
	}
	if request != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	if err != nil {
		return err
	}
	if int64(len(b)) > maxResponse {
		return errors.New("http: response too large")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("http: response status %d", res.StatusCode)
	}
	if response == nil || res.StatusCode == stdhttp.StatusNoContent {
		return nil
	}
	return json.Unmarshal(b, response)
}
