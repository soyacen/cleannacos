package cleannacos

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// DSN defaults applied by parseDSN.
const (
	// defaultPort is the Nacos server port used when the DSN omits one.
	defaultPort = 8848
	// defaultGroup is the Nacos group used when neither the DSN nor a
	// nacos-group tag provides one.
	defaultGroup = "DEFAULT_GROUP"
	// defaultLogDir is where the Nacos SDK writes its log files.
	defaultLogDir = "/tmp/nacos/log"
	// defaultCacheDir is where the Nacos SDK keeps its local config cache.
	defaultCacheDir = "/tmp/nacos/cache"
)

// dsn holds the structured parameters of a nacos:// DSN.
//
// A DSN only describes the Nacos server and the defaults of the connection.
// Which configs are read is declared by the nacos-data-id struct tag, so the
// DSN path must stay empty.
//
// Optional SDK settings are pointers so that "not set" can be told apart from
// "explicitly set to the zero value"; only the settings that were provided are
// forwarded to the Nacos SDK.
type dsn struct {
	host      string
	port      uint64
	namespace string
	group     string
	username  string
	password  string

	logDir   string
	cacheDir string

	timeoutMs           *uint64
	logLevel            *string
	appName             *string
	notLoadCacheAtStart *bool
}

// parseDSN parses a nacos:// DSN into structured connection parameters.
//
//	nacos://user:pass@host:8848?namespace=ns&group=g&timeoutMs=5000
//
// The path must be empty: dataIds belong to the config structure, not to the
// DSN. Only single host DSNs are supported; unknown query parameters are
// ignored.
func parseDSN(raw string) (*dsn, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("cleannacos: parse dsn %q: %w", raw, err)
	}
	if u.Scheme != "nacos" {
		return nil, fmt.Errorf("cleannacos: unsupported scheme %q in dsn %q, only \"nacos\" is supported", u.Scheme, raw)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("cleannacos: dsn %q has no host", raw)
	}
	if path := strings.Trim(u.Path, "/"); path != "" {
		return nil, fmt.Errorf("cleannacos: dsn %q must not carry a path, the dataId is declared with the %q struct tag", raw, TagNacosDataID)
	}

	d := &dsn{
		host:      u.Hostname(),
		port:      defaultPort,
		namespace: "",
		group:     defaultGroup,
		logDir:    defaultLogDir,
		cacheDir:  defaultCacheDir,
	}

	if port := u.Port(); port != "" {
		value, err := strconv.ParseUint(port, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cleannacos: invalid port %q in dsn %q: %w", port, raw, err)
		}
		d.port = value
	}

	if u.User != nil {
		d.username = u.User.Username()
		d.password, _ = u.User.Password()
	}

	query := u.Query()
	if v := query.Get("namespace"); v != "" {
		d.namespace = v
	}
	if v := query.Get("group"); v != "" {
		d.group = v
	}
	if v := query.Get("logDir"); v != "" {
		d.logDir = v
	}
	if v := query.Get("cacheDir"); v != "" {
		d.cacheDir = v
	}
	if v := query.Get("timeoutMs"); v != "" {
		value, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cleannacos: invalid timeoutMs %q in dsn %q: %w", v, raw, err)
		}
		d.timeoutMs = &value
	}
	if v := query.Get("logLevel"); v != "" {
		value := v
		d.logLevel = &value
	}
	if v := query.Get("appName"); v != "" {
		value := v
		d.appName = &value
	}
	if v := query.Get("notLoadCacheAtStart"); v != "" {
		value, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("cleannacos: invalid notLoadCacheAtStart %q in dsn %q: %w", v, raw, err)
		}
		d.notLoadCacheAtStart = &value
	}

	return d, nil
}

// serverIdent describes the Nacos server for DSN level error messages.
func (d *dsn) serverIdent() string {
	return fmt.Sprintf("server %s:%d (group %q, namespace %q)", d.host, d.port, d.group, d.namespace)
}
