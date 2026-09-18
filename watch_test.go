package cleannacos

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const watchDSN = "nacos://127.0.0.1:8848"

// watchConfig is watched from two dataIds.
type watchConfig struct {
	Server struct {
		Addr string `yaml:"addr" nacos-default:"server-fallback"`
	} `nacos-data-id:"server.yaml"`

	Database struct {
		Host string `yaml:"host" nacos-default:"db-fallback"`
	} `nacos-data-id:"db.yaml"`
}

// newWatchFake returns a fake client with a baseline for both sources.
func newWatchFake() *fakeClient {
	fake := newFakeClient()
	fake.setContent("server.yaml", "addr: base\n")
	fake.setContent("db.yaml", "host: db-base\n")

	return fake
}

func TestWatchDeliversBaselineSynchronously(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	notifications := make(chan *watchConfig, 8)
	stop, err := Watch(context.Background(), watchDSN, func(conf *watchConfig) {
		notifications <- conf
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	select {
	case baseline := <-notifications:
		if baseline.Server.Addr != "base" || baseline.Database.Host != "db-base" {
			t.Fatalf("baseline = %+v, want both sources merged", baseline)
		}
	default:
		t.Fatal("baseline was not delivered synchronously by Watch")
	}
}

func TestWatchRegistersListenersBeforeFetching(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	stop, err := Watch(context.Background(), watchDSN, func(*watchConfig) {})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	calls := fake.callOrder()
	if len(calls) != 4 {
		t.Fatalf("client calls = %v, want two listen and two get calls", calls)
	}
	for i, want := range []string{"listen", "listen", "get", "get"} {
		if calls[i] != want {
			t.Fatalf("client calls = %v, want listeners registered before the first fetch", calls)
		}
	}

	if got, want := fake.listenDataIDs(), []string{"db.yaml", "server.yaml"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("listeners = %v, want %v", got, want)
	}
}

func TestWatchNotifiesOnChange(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	notifications := make(chan *watchConfig, 8)
	stop, err := Watch(context.Background(), watchDSN, func(conf *watchConfig) {
		notifications <- conf
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	baseline := waitForNotification(t, notifications, 5*time.Second)

	if !fake.trigger("db.yaml", "host: db-changed\n") {
		t.Fatal("no listener was registered for db.yaml")
	}

	updated := waitForNotification(t, notifications, 5*time.Second)
	if updated.Database.Host != "db-changed" {
		t.Fatalf("updated Database.Host = %q, want %q", updated.Database.Host, "db-changed")
	}
	if updated.Server.Addr != "base" {
		t.Fatalf("updated Server.Addr = %q, want the unchanged source to survive", updated.Server.Addr)
	}
	if updated == baseline {
		t.Fatal("Watch reused the baseline object instead of building a new one")
	}
}

func TestWatchNotifiesOnScopedSource(t *testing.T) {
	fake := newFakeClient()
	fake.setContentIn("dev", "APP", "app.yaml", "addr: base\n")
	installFakeClient(t, fake)

	type scopedConfig struct {
		Server struct {
			Addr string `yaml:"addr"`
		} `nacos-data-id:"app.yaml" nacos-group:"APP" nacos-namespace:"dev"`
	}

	notifications := make(chan *scopedConfig, 8)
	stop, err := Watch(context.Background(), watchDSN, func(conf *scopedConfig) {
		notifications <- conf
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	if baseline := waitForNotification(t, notifications, 5*time.Second); baseline.Server.Addr != "base" {
		t.Fatalf("baseline Server.Addr = %q, want %q", baseline.Server.Addr, "base")
	}

	if !fake.triggerIn("dev", "APP", "app.yaml", "addr: changed\n") {
		t.Fatal("no listener was registered for the scoped source")
	}
	if updated := waitForNotification(t, notifications, 5*time.Second); updated.Server.Addr != "changed" {
		t.Fatalf("updated Server.Addr = %q, want %q", updated.Server.Addr, "changed")
	}
}

func TestWatchDedupesIdenticalContent(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	notifications := make(chan *watchConfig, 8)
	stop, err := Watch(context.Background(), watchDSN, func(conf *watchConfig) {
		notifications <- conf
	})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	waitForNotification(t, notifications, 5*time.Second)

	if !fake.trigger("server.yaml", "addr: base\n") {
		t.Fatal("no listener was registered")
	}
	assertNoNotification(t, notifications, 300*time.Millisecond)
}

func TestWatchBadContentKeepsBaseline(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	notifications := make(chan *watchConfig, 8)
	failures := make(chan error, 8)

	stop, err := Watch(context.Background(), watchDSN, func(conf *watchConfig) {
		notifications <- conf
	}, WithErrorHandler(func(err error) {
		failures <- err
	}))
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	waitForNotification(t, notifications, 5*time.Second)

	fake.trigger("server.yaml", "addr: [broken\n")

	select {
	case err := <-failures:
		if !strings.Contains(err.Error(), "cleannacos: parse") {
			t.Fatalf("error handler got %q, want a cleannacos parse error", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the error handler was not called for broken content")
	}
	assertNoNotification(t, notifications, 200*time.Millisecond)

	// Reverting to the last good content is not a change.
	fake.trigger("server.yaml", "addr: base\n")
	assertNoNotification(t, notifications, 300*time.Millisecond)
}

func TestWatchStopIsIdempotent(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	stop, err := Watch(context.Background(), watchDSN, func(*watchConfig) {})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop() error = %v", err)
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("second stop() error = %v, want nil", err)
	}

	if got, want := fake.cancelDataIDs(), []string{"db.yaml", "server.yaml"}; !reflect.DeepEqual(got, want) {
		t.Errorf("cancelled dataIds = %v, want %v", got, want)
	}
	if count := fake.countCalls("close"); count != 1 {
		t.Errorf("close calls = %d, want 1", count)
	}
	if fake.hasListener() {
		t.Error("listeners are still registered after stop")
	}
}

func TestWatchContextCancelStopsQuietly(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	notifications := make(chan *watchConfig, 8)
	failures := make(chan error, 8)

	ctx, cancel := context.WithCancel(context.Background())
	stop, err := Watch(ctx, watchDSN, func(conf *watchConfig) {
		notifications <- conf
	}, WithErrorHandler(func(err error) {
		failures <- err
	}))
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	waitForNotification(t, notifications, 5*time.Second)

	cancel()
	waitForCondition(t, 5*time.Second, fake.isClosed)

	if fake.hasListener() {
		t.Error("listeners are still registered after the context was cancelled")
	}
	select {
	case err := <-failures:
		t.Fatalf("error handler got %v, want silence on context cancellation", err)
	default:
	}
	if err := stop(context.Background()); err != nil {
		t.Fatalf("stop() after context cancellation error = %v, want nil", err)
	}
}

func TestWatchBaselineFailuresCleanUp(t *testing.T) {
	tests := []struct {
		name    string
		fake    *fakeClient
		wantMsg string
	}{
		{
			name: "provider error",
			fake: func() *fakeClient {
				fake := newWatchFake()
				fake.getErr = errors.New("boom")

				return fake
			}(),
			wantMsg: "boom",
		},
		{
			name: "unparsable content",
			fake: func() *fakeClient {
				fake := newWatchFake()
				fake.setContent("server.yaml", "addr: [broken\n")

				return fake
			}(),
			wantMsg: "cleannacos: parse",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			installFakeClient(t, tt.fake)

			stop, err := Watch(context.Background(), watchDSN, func(*watchConfig) {})
			if err == nil {
				t.Fatal("Watch() = nil, want error")
			}
			if stop != nil {
				t.Error("Watch() returned a StopFunc for a failed watch")
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("Watch() error = %q, want it to contain %q", err, tt.wantMsg)
			}
			if !tt.fake.isClosed() {
				t.Error("Watch() did not close the client after a failed startup")
			}
			if got, want := tt.fake.cancelDataIDs(), []string{"db.yaml", "server.yaml"}; !reflect.DeepEqual(got, want) {
				t.Errorf("cancelled dataIds = %v, want %v", got, want)
			}
		})
	}
}

func TestWatchRejectsNilNotify(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	stop, err := Watch[watchConfig](context.Background(), watchDSN, nil)
	if err == nil {
		t.Fatal("Watch() = nil, want error")
	}
	if stop != nil {
		t.Error("Watch() returned a StopFunc for an invalid call")
	}
	if len(fake.callOrder()) != 0 {
		t.Fatalf("client calls = %v, want none", fake.callOrder())
	}
}

func TestWatchRejectsCancelledContext(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Watch(ctx, watchDSN, func(*watchConfig) {})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Watch() error = %v, want context.Canceled", err)
	}
	if len(fake.callOrder()) != 0 {
		t.Fatalf("client calls = %v, want none", fake.callOrder())
	}
}

func TestWatchRejectsNonStruct(t *testing.T) {
	fake := newWatchFake()
	installFakeClient(t, fake)

	_, err := Watch(context.Background(), watchDSN, func(*string) {})
	if err == nil {
		t.Fatal("Watch() = nil, want error")
	}
	if len(fake.callOrder()) != 0 {
		t.Fatalf("client calls = %v, want none", fake.callOrder())
	}
}

func TestWatchStopReportsCancelError(t *testing.T) {
	fake := newWatchFake()
	fake.cancelErr = errors.New("cancel failed")
	installFakeClient(t, fake)

	stop, err := Watch(context.Background(), watchDSN, func(*watchConfig) {})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}

	err = stop(context.Background())
	if err == nil {
		t.Fatal("stop() = nil, want error")
	}
	if !strings.Contains(err.Error(), "cancel failed") {
		t.Fatalf("stop() error = %q, want the cancel error", err)
	}
	if !fake.isClosed() {
		t.Error("stop() did not close the client")
	}
}
