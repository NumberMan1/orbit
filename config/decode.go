package config

import (
	"bytes"
	"errors"
	"io"
	"reflect"

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
	if err := validate(v); err != nil {
		return v, err
	}
	return v, nil
}

func validate[T interface{ Validate() error }](v T) error {
	r := reflect.ValueOf(v)
	if !r.IsValid() {
		return errors.New("config: null configuration")
	}
	switch r.Kind() {
	case reflect.Pointer, reflect.Interface, reflect.Map, reflect.Slice:
		if r.IsNil() {
			return errors.New("config: null configuration")
		}
	}
	return v.Validate()
}
