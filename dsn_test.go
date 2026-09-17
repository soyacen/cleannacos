package cleannacos

import (
	"reflect"
	"strings"
	"testing"
)

func ptrTo[T any](value T) *T {
	return &value
}

// baseDSN returns the parameters expected for a minimal DSN.
func baseDSN() *dsn {
	return &dsn{
		host:     "127.0.0.1",
		port:     defaultPort,
		dataID:   "config.yaml",
		group:    defaultGroup,
		logDir:   defaultLogDir,
		cacheDir: defaultCacheDir,
		format:   formatYAML,
	}
}

func TestParseDSN(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    *dsn
		wantErr bool
	}{
		{
			name: "minimal dsn uses defaults",
			raw:  "nacos://127.0.0.1/config.yaml",
			want: baseDSN(),
		},
		{
			name: "namespace defaults to the public namespace",
			raw:  "nacos://127.0.0.1:8848/config.yaml",
			want: baseDSN(),
		},
		{
			name: "every query parameter is applied",
			raw:  "nacos://10.0.0.1:8849/app.yml?namespace=dev&group=APP&timeoutMs=5000&logDir=/tmp/log&cacheDir=/tmp/cache&logLevel=debug&notLoadCacheAtStart=true&appName=my-app&allowEmpty=true",
			want: func() *dsn {
				want := baseDSN()
				want.host = "10.0.0.1"
				want.port = 8849
				want.dataID = "app.yml"
				want.namespace = "dev"
				want.group = "APP"
				want.logDir = "/tmp/log"
				want.cacheDir = "/tmp/cache"
				want.logLevel = ptrTo("debug")
				want.timeoutMs = ptrTo(uint64(5000))
				want.notLoadCacheAtStart = ptrTo(true)
				want.appName = ptrTo("my-app")
				want.allowEmpty = true

				return want
			}(),
		},
		{
			name: "username and password",
			raw:  "nacos://admin:nacos@127.0.0.1:8848/config.yaml",
			want: func() *dsn {
				want := baseDSN()
				want.username = "admin"
				want.password = "nacos"

				return want
			}(),
		},
		{
			name: "username without password",
			raw:  "nacos://admin@127.0.0.1:8848/config.yaml",
			want: func() *dsn {
				want := baseDSN()
				want.username = "admin"

				return want
			}(),
		},
		{
			name: "dataId keeps its slashes",
			raw:  "nacos://127.0.0.1:8848/group/app.json",
			want: func() *dsn {
				want := baseDSN()
				want.dataID = "group/app.json"
				want.format = formatJSON

				return want
			}(),
		},
		{
			name: "extension case is ignored",
			raw:  "nacos://127.0.0.1:8848/APP.YAML",
			want: func() *dsn {
				want := baseDSN()
				want.dataID = "APP.YAML"
				want.format = formatYAML

				return want
			}(),
		},
		{
			name: "toml extension",
			raw:  "nacos://127.0.0.1:8848/app.toml",
			want: func() *dsn {
				want := baseDSN()
				want.dataID = "app.toml"
				want.format = formatTOML

				return want
			}(),
		},
		{
			name: "env extension",
			raw:  "nacos://127.0.0.1:8848/app.env",
			want: func() *dsn {
				want := baseDSN()
				want.dataID = "app.env"
				want.format = formatENV

				return want
			}(),
		},
		{
			name: "edn extension",
			raw:  "nacos://127.0.0.1:8848/app.edn",
			want: func() *dsn {
				want := baseDSN()
				want.dataID = "app.edn"
				want.format = formatEDN

				return want
			}(),
		},
		{
			name: "unknown query parameters are ignored",
			raw:  "nacos://127.0.0.1:8848/config.yaml?foo=bar&allowEmpty=true",
			want: func() *dsn {
				want := baseDSN()
				want.allowEmpty = true

				return want
			}(),
		},
		{name: "empty dsn", raw: "", wantErr: true},
		{name: "unsupported scheme", raw: "http://127.0.0.1:8848/config.yaml", wantErr: true},
		{name: "missing host", raw: "nacos:///config.yaml", wantErr: true},
		{name: "missing dataId", raw: "nacos://127.0.0.1:8848/", wantErr: true},
		{name: "missing extension", raw: "nacos://127.0.0.1:8848/config", wantErr: true},
		{name: "unknown extension", raw: "nacos://127.0.0.1:8848/config.txt", wantErr: true},
		{name: "non numeric port", raw: "nacos://127.0.0.1:port/config.yaml", wantErr: true},
		{name: "out of range port", raw: "nacos://127.0.0.1:99999999999999999999/config.yaml", wantErr: true},
		{name: "invalid timeoutMs", raw: "nacos://127.0.0.1:8848/config.yaml?timeoutMs=abc", wantErr: true},
		{name: "invalid notLoadCacheAtStart", raw: "nacos://127.0.0.1:8848/config.yaml?notLoadCacheAtStart=yes", wantErr: true},
		{name: "invalid allowEmpty", raw: "nacos://127.0.0.1:8848/config.yaml?allowEmpty=maybe", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDSN(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseDSN(%q) = %+v, want error", tt.raw, got)
				}
				if !strings.HasPrefix(err.Error(), "cleannacos:") {
					t.Fatalf("parseDSN(%q) error = %q, want cleannacos: prefix", tt.raw, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("parseDSN(%q) unexpected error: %v", tt.raw, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("parseDSN(%q)\n got %+v\nwant %+v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestDSNIdent(t *testing.T) {
	d := &dsn{dataID: "app.yaml", group: "APP", namespace: "dev"}

	want := `config "app.yaml" (group "APP", namespace "dev")`
	if got := d.ident(); got != want {
		t.Fatalf("ident() = %q, want %q", got, want)
	}
}
