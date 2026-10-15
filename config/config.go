// Package config loads strictly validated YAML without process-wide state.
package config

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"time"
)

const MaxFileSize = 1 << 20

type Config struct {
	Name            string        `yaml:"name"`
	TCP             string        `yaml:"tcp"`
	HTTP            string        `yaml:"http"`
	WebSocket       string        `yaml:"websocket"`
	MaxPayload      int           `yaml:"max_payload"`
	SendQueue       int           `yaml:"send_queue"`
	ReadTimeout     time.Duration `yaml:"read_timeout"`
	WriteTimeout    time.Duration `yaml:"write_timeout"`
	ShutdownTimeout time.Duration `yaml:"shutdown_timeout"`
}

func (c Config) Validate() error {
	if c.Name == "" {
		return errors.New("config: name required")
	}
	if c.TCP == "" && c.HTTP == "" && c.WebSocket == "" {
		return errors.New("config: at least one listener required")
	}
	for _, address := range []string{c.TCP, c.HTTP, c.WebSocket} {
		if address == "" {
			continue
		}
		_, port, err := net.SplitHostPort(address)
		if err != nil {
			return fmt.Errorf("config: invalid listener: %w", err)
		}
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return errors.New("config: invalid port")
		}
	}
	if c.MaxPayload < 1 || c.MaxPayload > 65535 || c.SendQueue < 1 {
		return errors.New("config: invalid payload or queue limit")
	}
	if c.ReadTimeout < 0 || c.WriteTimeout <= 0 || c.ShutdownTimeout <= 0 {
		return errors.New("config: invalid timeouts")
	}
	return nil
}

// Load also supports application-specific configuration types with Validate.
func Load[T interface{ Validate() error }](path string) (T, error) {
	var zero T
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	return Decode[T](f)
}

func Decode[T interface{ Validate() error }](r io.Reader) (T, error) {
	var value T
	b, err := io.ReadAll(io.LimitReader(r, MaxFileSize+1))
	if err != nil {
		return value, err
	}
	if len(b) > MaxFileSize {
		return value, errors.New("config: file too large")
	}
	return decode[T](b)
}
