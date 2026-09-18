package cleannacos

import (
	"context"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// parseIntoField parses value into a fresh field of the given type.
func parseIntoField(t *testing.T, valueType reflect.Type, value string, separator string, layout *string) (reflect.Value, error) {
	t.Helper()

	field := reflect.New(valueType).Elem()
	err := parseValue(field, value, separator, layout)

	return field, err
}

func TestParseValueScalars(t *testing.T) {
	tests := []struct {
		name  string
		value string
		parse func(t *testing.T, value string) reflect.Value
		want  interface{}
	}{
		{
			name:  "string",
			value: "hello",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(""), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse string: %v", err)
				}

				return field
			},
			want: "hello",
		},
		{
			name:  "bool",
			value: "true",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(false), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse bool: %v", err)
				}

				return field
			},
			want: true,
		},
		{
			name:  "int",
			value: "42",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(int32(0)), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse int32: %v", err)
				}

				return field.Convert(reflect.TypeOf(int32(0)))
			},
			want: int32(42),
		},
		{
			name:  "uint",
			value: "7",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(uint16(0)), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse uint16: %v", err)
				}

				return field
			},
			want: uint16(7),
		},
		{
			name:  "float",
			value: "1.5",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(float32(0)), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse float32: %v", err)
				}

				return field
			},
			want: float32(1.5),
		},
		{
			name:  "duration",
			value: "3s",
			parse: func(t *testing.T, value string) reflect.Value {
				field, err := parseIntoField(t, reflect.TypeOf(time.Duration(0)), value, DefaultSeparator, nil)
				if err != nil {
					t.Fatalf("parse duration: %v", err)
				}

				return field
			},
			want: 3 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.parse(t, tt.value)
			if !reflect.DeepEqual(got.Interface(), tt.want) {
				t.Fatalf("parsed %s = %v, want %v", tt.name, got.Interface(), tt.want)
			}
		})
	}
}

func TestParseValueTimeLayout(t *testing.T) {
	layout := "2006-01-02"
	field, err := parseIntoField(t, timeType, "2026-09-17", DefaultSeparator, &layout)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}

	want := time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)
	if !field.Interface().(time.Time).Equal(want) {
		t.Fatalf("parsed time = %s, want %s", field.Interface(), want)
	}

	// Without a layout the RFC3339 layout is used.
	field, err = parseIntoField(t, timeType, "2026-09-17T01:02:03Z", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	if got := field.Interface().(time.Time).UTC(); !got.Equal(time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC)) {
		t.Fatalf("parsed time = %s, want the RFC3339 value", got)
	}
}

func TestParseValueURLAndLocation(t *testing.T) {
	field, err := parseIntoField(t, urlType, "https://example.com:8443/path", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if got := field.Interface().(url.URL); got.Host != "example.com:8443" {
		t.Fatalf("parsed url = %+v, want host example.com:8443", got)
	}

	field, err = parseIntoField(t, locationPtrType, "UTC", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse location: %v", err)
	}
	if got := field.Interface().(*time.Location); got.String() != "UTC" {
		t.Fatalf("parsed location = %s, want UTC", got)
	}
}

func TestParseValueCollections(t *testing.T) {
	field, err := parseIntoField(t, reflect.TypeOf([]string{}), "a|b|c", "|", nil)
	if err != nil {
		t.Fatalf("parse slice: %v", err)
	}
	if got := field.Interface().([]string); !reflect.DeepEqual(got, []string{"a", "b", "c"}) {
		t.Fatalf("parsed slice = %v, want a|b|c split on the separator", got)
	}

	field, err = parseIntoField(t, reflect.TypeOf([]string{}), "   ", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse blank slice: %v", err)
	}
	if got := field.Interface().([]string); len(got) != 0 {
		t.Fatalf("parsed blank slice = %v, want an empty slice", got)
	}

	field, err = parseIntoField(t, reflect.TypeOf([]byte{}), "bytes", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse bytes: %v", err)
	}
	if got := string(field.Interface().([]byte)); got != "bytes" {
		t.Fatalf("parsed bytes = %q, want %q", got, "bytes")
	}

	field, err = parseIntoField(t, reflect.TypeOf(map[string]string{}), "k1:v1;k2:v2", ";", nil)
	if err != nil {
		t.Fatalf("parse map: %v", err)
	}
	want := map[string]string{"k1": "v1", "k2": "v2"}
	if got := field.Interface().(map[string]string); !reflect.DeepEqual(got, want) {
		t.Fatalf("parsed map = %v, want %v", got, want)
	}

	field, err = parseIntoField(t, reflect.TypeOf(map[string]int{}), "a:1,b:2", DefaultSeparator, nil)
	if err != nil {
		t.Fatalf("parse typed map: %v", err)
	}
	if got := field.Interface().(map[string]int); got["a"] != 1 || got["b"] != 2 {
		t.Fatalf("parsed map = %v, want a:1 and b:2", got)
	}
}

func TestParseValueErrors(t *testing.T) {
	tests := []struct {
		name      string
		valueType reflect.Type
		value     string
		separator string
		wantMsg   string
	}{
		{name: "invalid bool", valueType: reflect.TypeOf(false), value: "maybe", separator: DefaultSeparator, wantMsg: "invalid syntax"},
		{name: "invalid int", valueType: reflect.TypeOf(int(0)), value: "abc", separator: DefaultSeparator, wantMsg: "invalid syntax"},
		{name: "invalid duration", valueType: reflect.TypeOf(time.Duration(0)), value: "abc", separator: DefaultSeparator, wantMsg: "invalid duration"},
		{name: "invalid map item", valueType: reflect.TypeOf(map[string]string{}), value: "broken", separator: DefaultSeparator, wantMsg: "invalid map item"},
		{name: "unsupported type", valueType: reflect.TypeOf(struct{}{}), value: "x", separator: DefaultSeparator, wantMsg: "unsupported type"},
		{name: "unparsable time", valueType: timeType, value: "not-a-time", separator: DefaultSeparator, wantMsg: "cannot parse"},
		{name: "unknown location", valueType: locationPtrType, value: "Mars/Olympus", separator: DefaultSeparator, wantMsg: "unknown time zone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseIntoField(t, tt.valueType, tt.value, tt.separator, nil)
			if err == nil {
				t.Fatal("parseValue() = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("parseValue() error = %q, want it to contain %q", err, tt.wantMsg)
			}
		})
	}
}

func TestReadConfigBadDefaultNamesTheField(t *testing.T) {
	fake := newFakeClient()
	installFakeClient(t, fake)

	var cfg struct {
		App struct {
			Port int `yaml:"port" nacos-default:"not-a-number"`
		} `nacos-data-id:"app.yaml"`
	}

	err := ReadConfig(context.Background(), testDSN, &cfg)
	if err == nil {
		t.Fatal("ReadConfig() = nil, want a default parsing error")
	}
	if !strings.Contains(err.Error(), `field "App.Port"`) {
		t.Fatalf("ReadConfig() error = %q, want it to name the field", err)
	}
}
