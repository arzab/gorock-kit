package rockconfig

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"gopkg.in/yaml.v3"
)

// Decoder parses raw file bytes into a generic map.
// Implement this interface to add support for additional config formats.
type Decoder interface {
	Decode(data []byte) (map[string]interface{}, error)
}

var (
	decodersMu sync.RWMutex
	decoders   = map[string]Decoder{
		".json": &jsonDecoder{},
		".yaml": &yamlDecoder{},
		".yml":  &yamlDecoder{},
	}
)

// RegisterDecoder registers a Decoder for the given file extension (e.g. ".toml").
// Overwrites any existing decoder for that extension. Safe for concurrent use.
func RegisterDecoder(ext string, dec Decoder) {
	decodersMu.Lock()
	defer decodersMu.Unlock()
	decoders[ext] = dec
}

// load reads the file at path, expands $ENV_VAR references, and decodes it
// into a generic map using the decoder matched by file extension.
func load(path string) (map[string]interface{}, error) {
	ext := filepath.Ext(path)

	decodersMu.RLock()
	dec, ok := decoders[ext]
	decodersMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unsupported config file extension %q", ext)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	expanded := os.ExpandEnv(string(raw))

	m, err := dec.Decode([]byte(expanded))
	if err != nil {
		return nil, fmt.Errorf("decode config (%s): %w", ext, err)
	}

	return m, nil
}

// jsonDecoder decodes JSON config files.
type jsonDecoder struct{}

func (d *jsonDecoder) Decode(data []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// yamlDecoder decodes YAML config files.
type yamlDecoder struct{}

func (d *yamlDecoder) Decode(data []byte) (map[string]interface{}, error) {
	var m map[string]interface{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}
