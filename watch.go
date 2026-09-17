package cleannacos

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
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

// Watch listens for changes of the Nacos config addressed by dsn and hands a
// freshly merged *T to notify.
//
// The Nacos listener is registered first, then the current content is fetched
// and delivered synchronously as the baseline snapshot, so notify has returned
// at least once when Watch returns. Later changes are delivered asynchronously
// with one new *T per change; notify calls are serialized internally, so the
// callback can simply swap an atomic pointer.
func Watch[T any](ctx context.Context, dsn string, notify func(conf *T), options ...WatchOption) (StopFunc, error) {
	if notify == nil {
		return nil, errors.New("cleannacos: notify func is nil")
	}

	d, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("cleannacos: %s: %w", d.ident(), err)
	}

	opts := watchOptions{onError: defaultErrorHandler(d)}
	for _, option := range options {
		if option != nil {
			option(&opts)
		}
	}

	client, err := newClient(d)
	if err != nil {
		return nil, err
	}

	w := &watcher[T]{
		client:  client,
		dsn:     d,
		notify:  notify,
		onError: opts.onError,
		done:    make(chan struct{}),
	}

	if err := w.start(ctx); err != nil {
		// Neither the listener nor the client may outlive a failed startup.
		_ = w.stop(context.Background())
		return nil, err
	}

	return w.stop, nil
}

// watcher fetches config content, remembers the last content that was parsed
// successfully and serializes the notifications to the caller.
type watcher[T any] struct {
	client  configClient
	dsn     *dsn
	notify  func(conf *T)
	onError ErrFunc

	mu          sync.Mutex
	lastContent string

	once    sync.Once
	stopErr error
	done    chan struct{}
}

// start registers the listener and delivers the baseline snapshot.
func (w *watcher[T]) start(ctx context.Context) error {
	if err := w.client.ListenConfig(w.listenParam()); err != nil {
		return fmt.Errorf("cleannacos: listen %s: %w", w.dsn.ident(), err)
	}

	go func() {
		select {
		case <-ctx.Done():
			// A cancelled caller context is a normal shutdown, so stop quietly.
			_ = w.stop(context.Background())
		case <-w.done:
		}
	}()

	content, err := fetch(w.client, w.dsn)
	if err != nil {
		return err
	}

	conf, err := w.build(content)
	if err != nil {
		return err
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	w.lastContent = content
	w.notify(conf)

	return nil
}

// onChange handles a Nacos config change event.
func (w *watcher[T]) onChange(_, _, _, content string) {
	w.mu.Lock()

	if content == w.lastContent {
		w.mu.Unlock()
		return
	}

	conf, err := w.build(content)
	if err != nil {
		w.mu.Unlock()
		// Keep the previous good content as the baseline, so a later change
		// back to it is not reported again.
		w.onError(err)
		return
	}

	w.lastContent = content
	w.notify(conf)
	w.mu.Unlock()
}

// build merges raw Nacos content into a fresh *T.
func (w *watcher[T]) build(content string) (*T, error) {
	conf := new(T)
	if err := merge(content, w.dsn, conf); err != nil {
		return nil, err
	}

	return conf, nil
}

// listenParam builds the listener registration parameter.
func (w *watcher[T]) listenParam() vo.ConfigParam {
	param := w.dsn.configParam()
	param.OnChange = w.onChange

	return param
}

// stop cancels the listener and releases the client. Repeated calls are no-ops
// returning the error of the first call.
func (w *watcher[T]) stop(_ context.Context) error {
	w.once.Do(func() {
		w.stopErr = w.teardown()
	})

	return w.stopErr
}

// teardown cancels the listener and closes the client.
//
// Note for readers running with the race detector: nacos-sdk-go v2.3.5 has a
// data race of its own in this path. RpcClient.Shutdown deletes the client from
// the package level client map without holding cMux, while CreateClient reads
// that map under cMux, so closing a client while the SDK listen loop is active
// can be reported as a race between those two SDK frames. Cancelling the
// listener first keeps the window small, but it cannot be closed from here.
func (w *watcher[T]) teardown() error {
	var errs []error

	if err := w.client.CancelListenConfig(w.dsn.configParam()); err != nil {
		errs = append(errs, fmt.Errorf("cleannacos: cancel listen %s: %w", w.dsn.ident(), err))
	}
	closeClient(w.client)
	close(w.done)

	return errors.Join(errs...)
}

// defaultErrorHandler logs watch errors to the default slog logger.
func defaultErrorHandler(d *dsn) ErrFunc {
	return func(err error) {
		slog.Error("cleannacos: watch error",
			slog.String("dataId", d.dataID),
			slog.String("group", d.group),
			slog.String("namespace", d.namespace),
			slog.String("error", err.Error()),
		)
	}
}
