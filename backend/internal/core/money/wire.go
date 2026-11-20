package money

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"io"
)

const maxObjectSize = 1024

// Every required field occurs exactly once and carries a string, including zero.
func stringFields(data []byte, keys ...string) ([]string, error) {
	if len(data) > maxObjectSize {
		return nil, ErrBounds
	}
	d := json.NewDecoder(bytes.NewReader(data))
	t, err := d.Token()
	if err != nil || t != json.Delim('{') {
		return nil, ErrEncoding
	}
	fields := make(map[string]string, len(keys))
	for d.More() {
		key, err := d.Token()
		if err != nil {
			return nil, ErrEncoding
		}
		k, ok := key.(string)
		if !ok {
			return nil, ErrEncoding
		}
		if _, exists := fields[k]; exists {
			return nil, ErrEncoding
		}
		v, err := d.Token()
		s, ok := v.(string)
		if err != nil || !ok {
			return nil, ErrEncoding
		}
		fields[k] = s
	}
	if _, err = d.Token(); err != nil {
		return nil, ErrEncoding
	}
	if _, err = d.Token(); err != io.EOF || len(fields) != len(keys) {
		return nil, ErrEncoding
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		s, ok := fields[k]
		if !ok {
			return nil, ErrEncoding
		}
		out[i] = s
	}
	return out, nil
}

func objectValue(v json.Marshaler) (driver.Value, error) {
	data, err := v.MarshalJSON()
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func scanObject(src any, dst json.Unmarshaler) error {
	s, err := textSource(src)
	if err != nil {
		return err
	}
	if len(s) > maxObjectSize {
		return ErrBounds
	}
	return dst.UnmarshalJSON([]byte(s))
}
