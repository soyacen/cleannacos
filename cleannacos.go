package cleannacos

import (
	"context"
	"fmt"
	"io"

	"github.com/ilyakaznacheev/cleanenv"
)

// Setter is a cleanenv custom value setter.
type Setter = cleanenv.Setter

// Updater is a cleanenv custom update hook.
type Updater = cleanenv.Updater

// cleanenv tags and separators are re-exported for drop-in compatibility.
const (
	// DefaultSeparator is a default list and map separator character.
	DefaultSeparator = cleanenv.DefaultSeparator
	// TagEnv name of the environment variable or a list of names.
	TagEnv = cleanenv.TagEnv
	// TagEnvLayout value parsing layout (for types like time.Time).
	TagEnvLayout = cleanenv.TagEnvLayout
	// TagEnvDefault default value.
	TagEnvDefault = cleanenv.TagEnvDefault
	// TagEnvSeparator custom list and map separator.
	TagEnvSeparator = cleanenv.TagEnvSeparator
	// TagEnvDescription environment variable description.
	TagEnvDescription = cleanenv.TagEnvDescription
	// TagEnvUpd flag to mark a field as updatable.
	TagEnvUpd = cleanenv.TagEnvUpd
	// TagEnvRequired flag to mark a field as required.
	TagEnvRequired = cleanenv.TagEnvRequired
	// TagEnvPrefix flag to specify prefix for structure fields.
	TagEnvPrefix = cleanenv.TagEnvPrefix
)

// Parsers re-exported from cleanenv.
var (
	// ParseYAML parses YAML from reader to data structure.
	ParseYAML = cleanenv.ParseYAML
	// ParseJSON parses JSON from reader to data structure.
	ParseJSON = cleanenv.ParseJSON
	// ParseTOML parses TOML from reader to data structure.
	ParseTOML = cleanenv.ParseTOML
)

// ReadConfig reads the Nacos config addressed by dsn into cfg.
//
// The merge order is the same as cleanenv.ReadConfig: Nacos content first,
// then environment variable overrides, then env-default values.
func ReadConfig(ctx context.Context, dsn string, cfg interface{}) error {
	return readConfig(ctx, dsn, cfg)
}

// UpdateConfig refetches the Nacos config addressed by dsn and merges it into
// cfg again. It is the manual refresh counterpart of a Watch.
func UpdateConfig(ctx context.Context, dsn string, cfg interface{}) error {
	return readConfig(ctx, dsn, cfg)
}

// readConfig implements ReadConfig and UpdateConfig.
func readConfig(ctx context.Context, rawDSN string, cfg interface{}) error {
	d, err := parseDSN(rawDSN)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("cleannacos: %s: %w", d.ident(), err)
	}

	client, err := newClient(d)
	if err != nil {
		return err
	}
	defer closeClient(client)

	content, err := fetch(client, d)
	if err != nil {
		return err
	}

	return merge(content, d, cfg)
}

// fetch reads the raw config content from Nacos.
func fetch(client configClient, d *dsn) (string, error) {
	content, err := client.GetConfig(d.configParam())
	if err != nil {
		return "", fmt.Errorf("cleannacos: get %s: %w", d.ident(), err)
	}
	if content == "" && !d.allowEmpty {
		return "", emptyConfigError(d)
	}

	return content, nil
}

// emptyConfigError mirrors cleanenv's "config file does not exist" failure.
func emptyConfigError(d *dsn) error {
	return fmt.Errorf("cleannacos: %s is empty or not found, set allowEmpty=true to read it as an empty config", d.ident())
}

// merge runs the cleanenv pipeline over the raw Nacos content:
// config content -> environment variables -> env-default values.
func merge(content string, d *dsn, cfg interface{}) error {
	if content != "" {
		if err := d.format.parse(content, cfg); err != nil {
			return fmt.Errorf("cleannacos: parse %s as %s: %w", d.ident(), d.format, err)
		}
	}

	return cleanenv.ReadEnv(cfg)
}

// ReadEnv reads environment variables into the structure. It behaves exactly
// like cleanenv.ReadEnv.
func ReadEnv(cfg interface{}) error {
	return cleanenv.ReadEnv(cfg)
}

// UpdateEnv rereads (updates) environment variables in the structure. It
// behaves exactly like cleanenv.UpdateEnv, including the env-upd semantics.
func UpdateEnv(cfg interface{}) error {
	return cleanenv.UpdateEnv(cfg)
}

// GetDescription returns a description of environment variables. It behaves
// exactly like cleanenv.GetDescription.
func GetDescription(cfg interface{}, headerText *string) (string, error) {
	return cleanenv.GetDescription(cfg, headerText)
}

// Usage returns a configuration usage help. It behaves exactly like
// cleanenv.Usage.
func Usage(cfg interface{}, headerText *string, usageFuncs ...func()) func() {
	return cleanenv.Usage(cfg, headerText, usageFuncs...)
}

// FUsage prints configuration help into the custom output. It behaves exactly
// like cleanenv.FUsage.
func FUsage(w io.Writer, cfg interface{}, headerText *string, usageFuncs ...func()) func() {
	return cleanenv.FUsage(w, cfg, headerText, usageFuncs...)
}
