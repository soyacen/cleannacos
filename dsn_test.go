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
		group:    defaultGroup,
		logDir:   defaultLogDir,
		cacheDir: defaultCacheDir,
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
			raw:  "nacos://127.0.0.1",
			want: baseDSN(),
		},
		{
			name: "namespace defaults to the public namespace",
			raw:  "nacos://127.0.0.1:8848",
			want: baseDSN(),
		},
		{
			name: "every query parameter is applied",
			raw:  "nacos://10.0.0.1:8849?namespace=dev&group=APP&timeoutMs=5000&logDir=/tmp/log&cacheDir=/tmp/cache&logLevel=debug&notLoadCacheAtStart=true&appName=my-app",
			want: func() *dsn {
				want := baseDSN()
				want.host = "10.0.0.1"
				want.port = 8849
				want.namespace = "dev"
				want.group = "APP"
				want.logDir = "/tmp/log"
				want.cacheDir = "/tmp/cache"
				want.logLevel = ptrTo("debug")
				want.timeoutMs = ptrTo(uint64(5000))
				want.notLoadCacheAtStart = ptrTo(true)
				want.appName = ptrTo("my-app")

				return want
			}(),
		},
		{
			name: "username and password",
			raw:  "nacos://admin:nacos@127.0.0.1:8848",
			want: func() *dsn {
				want := baseDSN()
				want.username = "admin"
				want.password = "nacos"

				return want
			}(),
		},
		{
			name: "username without password",
			raw:  "nacos://admin@127.0.0.1:8848",
			want: func() *dsn {
				want := baseDSN()
				want.username = "admin"

				return want
			}(),
		},
		{
			name: "unknown query parameters are ignored",
			raw:  "nacos://127.0.0.1:8848?foo=bar",
			want: baseDSN(),
		},
		{name: "empty dsn", raw: "", wantErr: true},
		{name: "unsupported scheme", raw: "http://127.0.0.1:8848", wantErr: true},
		{name: "missing host", raw: "nacos:///", wantErr: true},
		{name: "dataId in the path", raw: "nacos://127.0.0.1:8848/config.yaml", wantErr: true},
		{name: "dataId in a nested path", raw: "nacos://127.0.0.1:8848/group/app.yaml", wantErr: true},
		{name: "non numeric port", raw: "nacos://127.0.0.1:port", wantErr: true},
		{name: "out of range port", raw: "nacos://127.0.0.1:99999999999999999999", wantErr: true},
		{name: "invalid timeoutMs", raw: "nacos://127.0.0.1:8848?timeoutMs=abc", wantErr: true},
		{name: "invalid notLoadCacheAtStart", raw: "nacos://127.0.0.1:8848?notLoadCacheAtStart=yes", wantErr: true},
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

func TestParseDSNRejectsPath(t *testing.T) {
	_, err := parseDSN("nacos://127.0.0.1:8848/app.yaml")
	if err == nil {
		t.Fatal("parseDSN() = nil, want error")
	}
	if !strings.Contains(err.Error(), TagNacosDataID) {
		t.Fatalf("parseDSN() error = %q, want it to point at the %s tag", err, TagNacosDataID)
	}
}

func TestParseDSNIgnoresAllowEmpty(t *testing.T) {
	d, err := parseDSN("nacos://127.0.0.1:8848?allowEmpty=maybe")
	if err != nil {
		t.Fatalf("parseDSN() error = %v, want the removed allowEmpty parameter to be ignored", err)
	}
	if !reflect.DeepEqual(d, baseDSN()) {
		t.Fatalf("parseDSN() = %+v, want %+v", d, baseDSN())
	}
}

func TestServerIdent(t *testing.T) {
	d := &dsn{host: "127.0.0.1", port: 8848, group: "APP", namespace: "dev"}

	want := `server 127.0.0.1:8848 (group "APP", namespace "dev")`
	if got := d.serverIdent(); got != want {
		t.Fatalf("serverIdent() = %q, want %q", got, want)
	}
}

func TestSourceIdent(t *testing.T) {
	src := source{dataID: "app.yaml", group: "APP", namespace: "dev"}

	want := `config "app.yaml" (group "APP", namespace "dev")`
	if got := src.ident(); got != want {
		t.Fatalf("ident() = %q, want %q", got, want)
	}
}
