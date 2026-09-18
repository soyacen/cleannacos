package cleannacos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"sync"
)

// StopFunc stops a running watch. It is safe to call more than once.
type StopFunc func(ctx context.Context) error

// ErrFunc handles errors reported by a running watch.
type ErrFunc func(err error)

// watchOptions holds the resolved Watch options.
type watchOptions struct {
	onError ErrFunc
}

// WatchOption configures Watch.
type WatchOption func(*watchOptions)

// WithErrorHandler sets the handler invoked for watch errors, such as config
// content that cannot be parsed. Without it, errors are logged with slog.
func WithErrorHandler(errFunc ErrFunc) WatchOption {
	return func(options *watchOptions) {
		if errFunc != nil {
			options.onError = errFunc
		}
	}
}

// Watch listens for changes of every Nacos config declared by the struct tags
// of T and hands a freshly merged *T to notify.
//
// The listeners are registered first, then the current content of every source
// is fetched and delivered synchronously as the baseline snapshot, so notify
// has returned at least once when Watch returns. Later changes are delivered
// asynchronously with one new *T per change; notify calls are serialized
// internally, so the callback can simply swap an atomic pointer.
//
// A change that cannot be parsed keeps the previous snapshot and is reported to
// the error handler only.
func Watch[T any](ctx context.Context, dsn string, notify func(conf *T), options ...WatchOption) (StopFunc, error) {
	if notify == nil {
		return nil, errors.New("cleannacos: notify func is nil")
	}

	d, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cleannacos: %s: %w", d.serverIdent(), err)
	}

	conf := new(T)
	if reflect.TypeOf(conf).Elem().Kind() != reflect.Struct {
		return nil, fmt.Errorf("cleannacos: cfg must be a struct, got %T", conf)
	}

	schema, err := buildSchema(conf, d)
	if err != nil {
		return nil, err
	}

	opts := watchOptions{onError: defaultErrorHandler()}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}

	w := &watcher[T]{
		pool:     newClientPool(d),
		schema:   schema,
		notify:   notify,
		onError:  opts.onError,
		contents: make(map[string]string, len(schema.sources)),
		done:     make(chan struct{}),
	}

	if err := w.start(ctx); err != nil {
		// Neither the listeners nor the clients may outlive a failed startup.
		_ = w.stop(context.Background())
		return nil, err
	}

	return w.stop, nil
}

// watcher fetches every source, remembers the last content that was parsed
// successfully and serializes the notifications to the caller.
type watcher[T any] struct {
	pool    *clientPool
	schema  *schema
	notify  func(conf *T)
	onError ErrFunc

	mu       sync.Mutex
	contents map[string]string

	once    sync.Once
	stopErr error
	done    chan struct{}
}

// start registers the listeners and delivers the baseline snapshot.
func (w *watcher[T]) start(ctx context.Context) error {
	for _, src := range w.schema.sources {
		client, err := w.pool.client(src.namespace)
		if err != nil {
			return err
		}

		param := src.configParam()
		param.OnChange = w.onChange(src)
		if err := client.ListenConfig(param); err != nil {
			return fmt.Errorf("cleannacos: listen %s: %w", src.ident(), err)
		}
	}

	go func() {
		select {
		case <-ctx.Done():
			// A cancelled caller context is a normal shutdown, so stop quietly.
			_ = w.stop(context.Background())
		case <-w.done:
		}
	}()

	contents, err := fetchAll(w.pool, w.schema)
	if err != nil {
		return err
	}

	conf, err := build[T](w.schema, contents)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.contents = contents
	w.notify(conf)

	return nil
}

// onChange handles a Nacos config change event of one source.
func (w *watcher[T]) onChange(src source) func(namespace, group, dataID, content string) {
	return func(_, _, _, content string) {
		w.mu.Lock()
		defer w.mu.Unlock()

		if content == w.contents[src.key()] {
			return
		}

		contents, err := fetchAll(w.pool, w.schema)
		if err != nil {
			w.onError(err)

			return
		}
		if sameContents(w.contents, contents) {
			return
		}

		conf, err := build[T](w.schema, contents)
		if err != nil {
			// Keep the previous good contents, so a later change back to them
			// is not reported again.
			w.onError(err)

			return
		}

		w.contents = contents
		w.notify(conf)
	}
}

// build merges the contents of every source into a fresh *T.
func build[T any](schema *schema, contents map[string]string) (*T, error) {
	conf := new(T)
	if err := apply(schema, contents, reflect.ValueOf(conf).Elem()); err != nil {
		return nil, err
	}

	return conf, nil
}

// sameContents reports whether both content maps are equal.
func sameContents(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value {
			return false
		}
	}

	return true
}

// stop cancels the listeners and releases the clients. Repeated calls are
// no-ops returning the error of the first call.
func (w *watcher[T]) stop(_ context.Context) error {
	w.once.Do(func() {
		w.stopErr = w.teardown()
	})

	return w.stopErr
}

// teardown cancels every listener and closes every client.
//
// Note for readers running with the race detector: nacos-sdk-go v2.3.5 has a
// data race of its own in this path. RpcClient.Shutdown deletes the client from
// the package level client map without holding cMux, while CreateClient reads
// that map under cMux, so closing a client while the SDK listen loop is active
// can be reported as a race between those two SDK frames. Cancelling the
// listeners first keeps the window small, but it cannot be closed from here.
func (w *watcher[T]) teardown() error {
	var errs []error

	for _, src := range w.schema.sources {
		client, err := w.pool.client(src.namespace)
		if err != nil {
			errs = append(errs, err)

			continue
		}
		if err := client.CancelListenConfig(src.configParam()); err != nil {
			errs = append(errs, fmt.Errorf("cleannacos: cancel listen %s: %w", src.ident(), err))
		}
	}

	w.pool.closeAll()
	close(w.done)

	return errors.Join(errs...)
}

// defaultErrorHandler logs watch errors to the default slog logger.
func defaultErrorHandler() ErrFunc {
	return func(err error) {
		slog.Error("cleannacos: watch error", slog.String("error", err.Error()))
	}
}
