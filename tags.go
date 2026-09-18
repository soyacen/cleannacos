package cleannacos

import (
	"fmt"
	"reflect"
	"strings"
)

// Nacos struct tags recognised by this package. Unlike cleanenv there is no
// environment variable layer: every tag below describes how a field is read
// from Nacos.
const (
	// TagNacosDataID declares the dataId a field is read from. It may be used
	// on a field at any nesting level and applies to the whole subtree.
	TagNacosDataID = "nacos-data-id"
	// TagNacosGroup overrides the group of a field subtree.
	TagNacosGroup = "nacos-group"
	// TagNacosNamespace overrides the namespace of a field subtree.
	TagNacosNamespace = "nacos-namespace"
	// TagNacosDefault is the value used when a field is still at its zero
	// value after the config content was parsed.
	TagNacosDefault = "nacos-default"
	// TagNacosDescription documents a field for GetDescription.
	TagNacosDescription = "nacos-description"
	// TagNacosLayout is the layout used to convert a default value into a
	// time-like field.
	TagNacosLayout = "nacos-layout"
	// TagNacosRequired marks a field that must hold a value once defaults were
	// applied.
	TagNacosRequired = "nacos-required"
	// TagNacosSeparator is the separator used to convert a default value into
	// a slice or map field. It defaults to DefaultSeparator.
	TagNacosSeparator = "nacos-separator"
)

// source identifies a single Nacos config: a dataId inside a group and a
// namespace.
type source struct {
	dataID    string
	group     string
	namespace string
}

// key is the identity of a source inside one load.
func (s source) key() string {
	return s.namespace + "\x00" + s.group + "\x00" + s.dataID
}

// ident describes the source for error messages and logs.
func (s source) ident() string {
	return fmt.Sprintf("config %q (group %q, namespace %q)", s.dataID, s.group, s.namespace)
}

// fieldPath is the field index path from the root struct down to a field.
type fieldPath []int

// sourceRoot is a field whose subtree is parsed from its own source.
type sourceRoot struct {
	source source
	path   fieldPath
	goPath string
}

// leafField is a value field that takes part in default and required handling.
type leafField struct {
	source      source
	path        fieldPath
	goPath      string
	keyPath     string
	defValue    *string
	layout      *string
	separator   string
	required    bool
	description string
	kind        reflect.Kind
}

// schema is the tag metadata of a config structure.
type schema struct {
	roots   []sourceRoot
	leaves  []leafField
	sources []source
	index   map[string]bool
}

// buildSchema reads the struct tags of cfg. Sources inherit from the DSN
// defaults until a field declares a dataId of its own.
func buildSchema(cfg interface{}, d *dsn) (*schema, error) {
	value := reflect.ValueOf(cfg)
	if !value.IsValid() || value.Kind() != reflect.Ptr || value.IsNil() {
		return nil, fmt.Errorf("cleannacos: cfg must be a non-nil pointer to a struct, got %T", cfg)
	}

	root := value.Elem()
	if root.Kind() != reflect.Struct {
		return nil, fmt.Errorf("cleannacos: cfg must point to a struct, got %T", cfg)
	}

	b := &schemaBuilder{
		group:     d.group,
		namespace: d.namespace,
		schema:    &schema{index: make(map[string]bool)},
	}
	if err := b.walk(root.Type(), nil, nil, "", ""); err != nil {
		return nil, err
	}

	return b.schema, nil
}

// schemaBuilder walks a config structure and collects source roots and leaves.
type schemaBuilder struct {
	group     string
	namespace string
	schema    *schema
}

// walk visits the fields of a struct type. cur is the source inherited from
// the closest ancestor that declared a dataId, or nil when there is none.
func (b *schemaBuilder) walk(t reflect.Type, path fieldPath, cur *source, goPrefix, keyPrefix string) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			// Unexported fields are never touched.
			continue
		}

		goPath := goPrefix + field.Name
		keyPath := keyPrefix
		if name := keyName(field); name != "" {
			keyPath += name
		}
		path := appendPath(path, i)

		fieldSource := cur
		if dataID := field.Tag.Get(TagNacosDataID); dataID != "" {
			src := source{dataID: dataID, group: b.group, namespace: b.namespace}
			if group := field.Tag.Get(TagNacosGroup); group != "" {
				src.group = group
			}
			if namespace := field.Tag.Get(TagNacosNamespace); namespace != "" {
				src.namespace = namespace
			}

			fieldSource = &src
			b.addSource(src)
			b.schema.roots = append(b.schema.roots, sourceRoot{source: src, path: path, goPath: goPath})

			// The subtree is parsed from a document of its own, so the key path
			// restarts at the document root.
			keyPath = ""
		}

		if isStructLike(field.Type) {
			elem := field.Type
			if elem.Kind() == reflect.Ptr {
				elem = elem.Elem()
			}
			if err := b.walk(elem, path, fieldSource, goPath+".", subPrefix(keyPath)); err != nil {
				return err
			}

			continue
		}

		if fieldSource == nil {
			// Without a dataId the field has no source and stays untouched.
			continue
		}

		leaf := leafField{
			source:      *fieldSource,
			path:        path,
			goPath:      goPath,
			keyPath:     keyPath,
			separator:   DefaultSeparator,
			description: field.Tag.Get(TagNacosDescription),
			kind:        field.Type.Kind(),
		}
		if def, ok := field.Tag.Lookup(TagNacosDefault); ok {
			leaf.defValue = &def
		}
		if layout, ok := field.Tag.Lookup(TagNacosLayout); ok {
			leaf.layout = &layout
		}
		if separator, ok := field.Tag.Lookup(TagNacosSeparator); ok {
			leaf.separator = separator
		}
		_, leaf.required = field.Tag.Lookup(TagNacosRequired)

		b.schema.leaves = append(b.schema.leaves, leaf)
	}

	return nil
}

// subPrefix returns the key prefix of the fields nested one level deeper.
func subPrefix(prefix string) string {
	if prefix == "" {
		return ""
	}

	return prefix + "."
}

// addSource registers a source once.
func (b *schemaBuilder) addSource(src source) {
	if b.schema.index[src.key()] {
		return
	}

	b.schema.index[src.key()] = true
	b.schema.sources = append(b.schema.sources, src)
}

// keyName is the key a field uses inside a config document. The yaml tag wins,
// then json and toml, and the Go field name is the last resort. Embedded
// structs are inlined by the decoders, so they do not add a key segment.
func keyName(field reflect.StructField) string {
	for _, tag := range []string{"yaml", "json", "toml"} {
		name := field.Tag.Get(tag)
		if name == "" || name == "-" {
			continue
		}
		if idx := strings.Index(name, ","); idx >= 0 {
			name = name[:idx]
		}
		if name != "" {
			return name
		}
	}

	if field.Anonymous {
		return ""
	}

	return field.Name
}

// appendPath returns a new field path with index appended.
func appendPath(path fieldPath, index int) fieldPath {
	next := make(fieldPath, len(path)+1)
	copy(next, path)
	next[len(path)] = index

	return next
}

// fieldValue resolves a field path on the root struct value. When alloc is
// true, nil pointers along the way are allocated. When alloc is false and a
// pointer on the way is nil, the zero Value is returned.
func fieldValue(root reflect.Value, path fieldPath, alloc bool) (reflect.Value, error) {
	value := root

	for _, index := range path {
		if value.Kind() == reflect.Ptr {
			if value.IsNil() {
				if !alloc {
					return reflect.Value{}, nil
				}
				value.Set(reflect.New(value.Type().Elem()))
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct {
			return reflect.Value{}, fmt.Errorf("cleannacos: cannot resolve field path %v: %s is not a struct", path, value.Type())
		}

		value = value.Field(index)
	}

	return value, nil
}
