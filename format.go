package cleannacos

import (
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/ilyakaznacheev/cleanenv"
	"github.com/joho/godotenv"
	"olympos.io/encoding/edn"
)

// format is the configuration format implied by a dataId extension.
type format string

const (
	formatYAML format = "yaml"
	formatJSON format = "json"
	formatTOML format = "toml"
	formatENV  format = "env"
	formatEDN  format = "edn"
)

// supportedExtensions lists the dataId extensions accepted by this package.
var supportedExtensions = []string{".yaml", ".yml", ".json", ".toml", ".env", ".edn"}

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
	case ".env":
		return formatENV, nil
	case ".edn":
		return formatEDN, nil
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
		return cleanenv.ParseYAML(reader, cfg)
	case formatJSON:
		return cleanenv.ParseJSON(reader, cfg)
	case formatTOML:
		return cleanenv.ParseTOML(reader, cfg)
	case formatENV:
		return parseEnv(reader)
	case formatEDN:
		return edn.NewDecoder(reader).Decode(cfg)
	default:
		return fmt.Errorf("cleannacos: unsupported format %q", string(f))
	}
}

// parseEnv parses ENV content and writes every variable into the process
// environment, mirroring cleanenv's own .env handling. The values are picked up
// by the cleanenv.ReadEnv call that follows.
func parseEnv(r io.Reader) error {
	vars, err := godotenv.Parse(r)
	if err != nil {
		return err
	}

	for key, value := range vars {
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("set environment %q: %w", key, err)
		}
	}

	return nil
}
