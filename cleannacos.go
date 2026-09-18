package cleannacos

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ReadConfig reads every Nacos config declared by the struct tags of cfg into
// cfg.
//
// The DSN carries the server address only; the dataIds come from the
// nacos-data-id tag. Configs are parsed outermost first, so a nested
// nacos-data-id overrides the values of its parent document. Defaults are
// applied afterwards to fields that still hold their zero value.
func ReadConfig(ctx context.Context, dsn string, cfg interface{}) error {
	return readConfig(ctx, dsn, cfg)
}

// UpdateConfig refetches the Nacos configs declared by the struct tags of cfg
// and merges them into cfg again. It is the manual refresh counterpart of a
// Watch.
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
		return fmt.Errorf("cleannacos: %s: %w", d.serverIdent(), err)
	}

	schema, err := buildSchema(cfg, d)
	if err != nil {
		return err
	}
	if len(schema.sources) == 0 {
		return nil
	}

	pool := newClientPool(d)
	defer pool.closeAll()

	contents, err := fetchAll(pool, schema)
	if err != nil {
		return err
	}

	return apply(schema, contents, reflect.ValueOf(cfg).Elem())
}

// fetchAll reads the raw content of every source of the schema.
func fetchAll(pool *clientPool, schema *schema) (map[string]string, error) {
	contents := make(map[string]string, len(schema.sources))

	for _, src := range schema.sources {
		client, err := pool.client(src.namespace)
		if err != nil {
			return nil, err
		}

		content, err := client.GetConfig(src.configParam())
		if err != nil {
			return nil, fmt.Errorf("cleannacos: get %s: %w", src.ident(), err)
		}
		contents[src.key()] = content
	}

	return contents, nil
}

// apply parses every source into the config structure and then fills the
// missing values with defaults.
func apply(schema *schema, contents map[string]string, root reflect.Value) error {
	for _, sr := range schema.roots {
		content := contents[sr.source.key()]
		if content == "" {
			// An empty config is an empty document: defaults and required
			// checks decide what happens next.
			continue
		}

		f, err := resolveFormat(sr.source.dataID)
		if err != nil {
			return err
		}

		target, err := fieldValue(root, sr.path, true)
		if err != nil {
			return err
		}
		if err := parseInto(f, target, content); err != nil {
			return fmt.Errorf("cleannacos: parse %s as %s: %w", sr.source.ident(), f, err)
		}
	}

	for _, leaf := range schema.leaves {
		if err := applyLeaf(root, leaf); err != nil {
			return err
		}
	}

	return nil
}

// parseInto parses content into the field value behind target.
func parseInto(f format, target reflect.Value, content string) error {
	if target.Kind() == reflect.Ptr {
		value := reflect.New(target.Type().Elem())
		if err := f.parse(content, value.Interface()); err != nil {
			return err
		}
		target.Set(value)

		return nil
	}
	if !target.CanAddr() {
		return fmt.Errorf("cleannacos: field %s cannot be set", target.Type())
	}

	return f.parse(content, target.Addr().Interface())
}

// applyLeaf fills the default of a field and verifies nacos-required.
func applyLeaf(root reflect.Value, leaf leafField) error {
	value, err := fieldValue(root, leaf.path, false)
	if err != nil {
		return err
	}

	if !value.IsValid() || value.IsZero() {
		if leaf.defValue != nil {
			target, err := fieldValue(root, leaf.path, true)
			if err != nil {
				return err
			}
			if err := parseValue(target, *leaf.defValue, leaf.separator, leaf.layout); err != nil {
				return fmt.Errorf("cleannacos: field %q: parse default %q: %w", leaf.goPath, *leaf.defValue, err)
			}
			value = target
		}

		if leaf.required && (!value.IsValid() || value.IsZero()) {
			return fmt.Errorf("cleannacos: field %q is required but no value was found in %s", leaf.goPath, leaf.source.ident())
		}
	}

	return nil
}

// GetDescription returns a description of the Nacos config keys read into cfg.
//
// Every field that resolves to a dataId is listed as "dataId:keyPath",
// followed by its nacos-description, its default value and whether it is
// required. A custom header can be provided with headerText.
func GetDescription(cfg interface{}, headerText *string) (string, error) {
	schema, err := buildSchema(cfg, &dsn{group: defaultGroup})
	if err != nil {
		return "", err
	}

	header := "Nacos configuration:"
	if headerText != nil {
		header = *headerText
	}

	descriptions := make([]string, 0, len(schema.leaves))
	for _, leaf := range schema.leaves {
		label := leaf.source.dataID
		if leaf.keyPath != "" {
			label += ":" + leaf.keyPath
		}

		entry := fmt.Sprintf("\n  %s %s\n    \t%s", label, leaf.kind, leaf.description)
		if leaf.defValue != nil {
			entry += fmt.Sprintf(" (default %q)", *leaf.defValue)
		}
		if leaf.required {
			entry += " (required)"
		}

		descriptions = append(descriptions, entry)
	}

	if len(descriptions) == 0 {
		return "", nil
	}
	sort.Strings(descriptions)

	return header + strings.Join(descriptions, ""), nil
}
