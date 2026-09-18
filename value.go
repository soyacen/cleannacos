package cleannacos

import (
	"encoding"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// DefaultSeparator is the default list and map separator used when a default
// value is converted into a slice or map field.
const DefaultSeparator = ","

// Setter is a custom value setter. A field whose type (or pointer type)
// implements it receives the raw default value instead of the built-in
// conversion.
//
//	type MyField string
//
//	func (f *MyField) SetValue(s string) error {
//		*f = MyField("my field is: " + s)
//		return nil
//	}
type Setter interface {
	SetValue(string) error
}

var (
	timeType            = reflect.TypeOf(time.Time{})
	urlType             = reflect.TypeOf(url.URL{})
	locationPtrType     = reflect.TypeOf(&time.Location{})
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	setterType          = reflect.TypeOf((*Setter)(nil)).Elem()
)

// isSpecialStruct reports whether a struct type has its own scalar parser.
func isSpecialStruct(t reflect.Type) bool {
	switch t {
	case timeType, urlType, locationPtrType:
		return true
	default:
		return false
	}
}

// implementsSetter reports whether the type or its pointer implements Setter.
func implementsSetter(t reflect.Type) bool {
	if t.Implements(setterType) {
		return true
	}
	if t.Kind() != reflect.Ptr {
		return reflect.PointerTo(t).Implements(setterType)
	}

	return false
}

// implementsTextUnmarshaler reports whether the type or its pointer implements
// encoding.TextUnmarshaler.
func implementsTextUnmarshaler(t reflect.Type) bool {
	if t.Implements(textUnmarshalerType) {
		return true
	}
	if t.Kind() != reflect.Ptr {
		return reflect.PointerTo(t).Implements(textUnmarshalerType)
	}

	return false
}

// isStructLike reports whether a field is a structure whose fields are walked
// instead of being treated as a single value.
func isStructLike(t reflect.Type) bool {
	if isSpecialStruct(t) {
		return false
	}
	if t.Kind() == reflect.Ptr {
		if implementsTextUnmarshaler(t) || implementsSetter(t) {
			return false
		}
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return false
	}

	return !implementsTextUnmarshaler(t) && !implementsSetter(t)
}

// parseValue parses a raw string into the field, mirroring cleanenv semantics.
func parseValue(field reflect.Value, value, separator string, layout *string) error {
	valueType := field.Type()

	switch valueType {
	case timeType:
		l := time.RFC3339
		if layout != nil {
			l = *layout
		}
		parsed, err := time.Parse(l, value)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(parsed))

		return nil
	case urlType:
		parsed, err := url.Parse(value)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(*parsed))

		return nil
	case locationPtrType:
		location, err := time.LoadLocation(value)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(location))

		return nil
	}

	if field.CanInterface() {
		if unmarshaler, ok := field.Interface().(encoding.TextUnmarshaler); ok {
			return unmarshaler.UnmarshalText([]byte(value))
		} else if field.CanAddr() {
			if unmarshaler, ok := field.Addr().Interface().(encoding.TextUnmarshaler); ok {
				return unmarshaler.UnmarshalText([]byte(value))
			}
		}

		if setter, ok := field.Interface().(Setter); ok {
			return setter.SetValue(value)
		} else if field.CanAddr() {
			if setter, ok := field.Addr().Interface().(Setter); ok {
				return setter.SetValue(value)
			}
		}
	}

	switch valueType.Kind() {
	case reflect.String:
		field.SetString(value)

	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return err
		}
		field.SetBool(parsed)

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32:
		parsed, err := strconv.ParseInt(value, 0, valueType.Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)

	case reflect.Int64:
		if valueType == reflect.TypeOf(time.Duration(0)) {
			parsed, err := time.ParseDuration(value)
			if err != nil {
				return err
			}
			field.SetInt(int64(parsed))

			return nil
		}
		parsed, err := strconv.ParseInt(value, 0, valueType.Bits())
		if err != nil {
			return err
		}
		field.SetInt(parsed)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		parsed, err := strconv.ParseUint(value, 0, valueType.Bits())
		if err != nil {
			return err
		}
		field.SetUint(parsed)

	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, valueType.Bits())
		if err != nil {
			return err
		}
		field.SetFloat(parsed)

	case reflect.Slice:
		slice, err := parseSlice(valueType, value, separator, layout)
		if err != nil {
			return err
		}
		field.Set(slice)

	case reflect.Map:
		mapValue, err := parseMap(valueType, value, separator, layout)
		if err != nil {
			return err
		}
		field.Set(mapValue)

	default:
		return fmt.Errorf("unsupported type %s.%s", valueType.PkgPath(), valueType.Name())
	}

	return nil
}

// parseSlice parses a raw string into a slice value.
func parseSlice(valueType reflect.Type, value, separator string, layout *string) (reflect.Value, error) {
	if valueType.Elem().Kind() == reflect.Uint8 {
		return reflect.ValueOf([]byte(value)).Convert(valueType), nil
	}
	if strings.TrimSpace(value) == "" {
		return reflect.MakeSlice(valueType, 0, 0), nil
	}

	parts := strings.Split(value, separator)
	slice := reflect.MakeSlice(valueType, len(parts), len(parts))
	for i, part := range parts {
		if err := parseValue(slice.Index(i), part, separator, layout); err != nil {
			return reflect.Value{}, err
		}
	}

	return slice, nil
}

// parseMap parses a raw string into a map value.
func parseMap(valueType reflect.Type, value, separator string, layout *string) (reflect.Value, error) {
	mapValue := reflect.MakeMap(valueType)
	if strings.TrimSpace(value) == "" {
		return mapValue, nil
	}

	for _, pair := range strings.Split(value, separator) {
		kv := strings.SplitN(pair, ":", 2)
		if len(kv) != 2 {
			return reflect.Value{}, fmt.Errorf("invalid map item: %q", pair)
		}

		key := reflect.New(valueType.Key()).Elem()
		if err := parseValue(key, kv[0], separator, layout); err != nil {
			return reflect.Value{}, err
		}

		item := reflect.New(valueType.Elem()).Elem()
		if err := parseValue(item, kv[1], separator, layout); err != nil {
			return reflect.Value{}, err
		}

		mapValue.SetMapIndex(key, item)
	}

	return mapValue, nil
}
