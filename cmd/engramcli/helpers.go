package main

import (
	"bytes"
	"encoding/json"
	"io"
)

// jsonReader marshals v to JSON and returns an io.Reader. Panics on marshal
// failure (only used for trusted, compile-time-known structures).
func jsonReader(v any) io.Reader {
	data, err := json.Marshal(v)
	if err != nil {
		panic("jsonReader: " + err.Error())
	}
	return bytes.NewReader(data)
}
