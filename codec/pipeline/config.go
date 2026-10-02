package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// LoadConfig reads a JSON config file over DefaultConfig. Fields missing from the file
// keep their defaults; unknown fields are an error, so typos do not pass silently.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return cfg, fmt.Errorf("pipeline: config %s: %w", path, err)
	}
	return cfg, nil
}
