package cleannacos

import (
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

// format is the configuration format implied by a dataId extension.
type format string

const (
	formatYAML format = "yaml"
	formatJSON format = "json"
	formatTOML format = "toml"
)

// supportedExtensions lists the dataId extensions accepted by this package.
var supportedExtensions = []string{".yaml", ".yml", ".json", ".toml"}

// ParseYAML parses YAML from reader to data structure.
func ParseYAML(r io.Reader, cfg interface{}) error {
	return yaml.NewDecoder(r).Decode(cfg)
}

// ParseJSON parses JSON from reader to data structure.
func ParseJSON(r io.Reader, cfg interface{}) error {
	return json.NewDecoder(r).Decode(cfg)
}

// ParseTOML parses TOML from reader to data structure.
func ParseTOML(r io.Reader, cfg interface{}) error {
	_, err := toml.NewDecoder(r).Decode(cfg)

	return err
}

// resolveFormat maps the extension of a dataId to the parser handling it.
func resolveFormat(dataID string) (format, error) {
	supported := strings.Join(supportedExtensions, ", ")

	switch ext := strings.ToLower(path.Ext(dataID)); ext {
	case ".yaml", ".yml":
		return formatYAML, nil
	case ".json":
		return formatJSON, nil
	case ".toml":
		return formatTOML, nil
	case "":
		return "", fmt.Errorf("cleannacos: dataId %q has no extension, one of %s is required", dataID, supported)
	default:
		return "", fmt.Errorf("cleannacos: dataId %q has unsupported extension %q, one of %s is required", dataID, ext, supported)
	}
}

// parse parses raw config content into cfg using the parser of the format.
func (f format) parse(content string, cfg interface{}) error {
	reader := strings.NewReader(content)

	switch f {
	case formatYAML:
		return ParseYAML(reader, cfg)
	case formatJSON:
		return ParseJSON(reader, cfg)
	case formatTOML:
		return ParseTOML(reader, cfg)
	default:
		return fmt.Errorf("cleannacos: unsupported format %q", string(f))
	}
}
