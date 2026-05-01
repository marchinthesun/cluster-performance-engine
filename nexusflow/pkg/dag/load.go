package dag

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFile parses pipeline YAML from disk.
func LoadFile(path string) (*Pipeline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("yaml: %w", err)
	}
	if err := Validate(&doc.Pipeline); err != nil {
		return nil, err
	}
	return &doc.Pipeline, nil
}
