package config

import (
	"bytes"
	"errors"
	"io"

	"gopkg.in/yaml.v3"
)

func decode[T interface{ Validate() error }](b []byte) (T, error) {
	var v T
	d := yaml.NewDecoder(bytes.NewReader(b))
	d.KnownFields(true)
	if err := d.Decode(&v); err != nil {
		return v, err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		if err == nil {
			return v, errors.New("config: multiple YAML documents")
		}
		return v, err
	}
	if err := v.Validate(); err != nil {
		return v, err
	}
	return v, nil
}
