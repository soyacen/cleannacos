package cleannacos

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const watchDSN = "nacos://127.0.0.1:8848/watch.yaml"

type watchConfig struct {
	Host string `yaml:"host" env:"CLEANNACOS_TEST_WATCH_HOST" env-default:"fallback"`
}

func TestWatchDeliversBaselineSynchronously(t *testing.T) {
	fake := newFakeClient("host: base\n")
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
		if baseline.Host != "base" {
			t.Fatalf("baseline Host = %q, want %q", baseline.Host, "base")
		}
	default:
		t.Fatal("baseline was not delivered synchronously by Watch")
	}
}

func TestWatchRegistersListenerBeforeFetching(t *testing.T) {
	fake := newFakeClient("host: base\n")
	installFakeClient(t, fake)

	stop, err := Watch(context.Background(), watchDSN, func(*watchConfig) {})
	if err != nil {
		t.Fatalf("Watch() error = %v", err)
	}
	defer func() { _ = stop(context.Background()) }()

	calls := fake.callOrder()
	if len(calls) < 2 {
		t.Fatalf("client calls = %v, want a listen and a get call", calls)
	}
	if calls[0] != "listen" || calls[1] != "get" {
		t.Fatalf("client calls = %v, want the listener registered before the first fetch", calls)
	}
	if fake.lastListen.DataId != "watch.yaml" || fake.lastListen.Group != defaultGroup {
		t.Fatalf("listener param = %+v, want the DSN dataId and default group", fake.lastListen)
	}
}

func TestWatchNotifiesOnChange(t *testing.T) {
	fake := newFakeClient("host: base\n")
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

	done := make(chan struct{})
	go func() {
		defer close(done)
		fake.trigger("host: changed\n")
	}()

	updated := waitForNotification(t, notifications, 5*time.Second)
	<-done

	if updated.Host != "changed" {
		t.Fatalf("updated Host = %q, want %q", updated.Host, "changed")
	}
	if updated == baseline {
		t.Fatal("Watch reused the baseline object instead of building a new one")
	}
}

func TestWatchDedupesIdenticalContent(t *testing.T) {
	fake := newFakeClient("host: base\n")
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

	if !fake.trigger("host: base\n") {
		t.Fatal("no listener was registered")
	}
	assertNoNotification(t, notifications, 300*time.Millisecond)
}

func TestWatchBadContentKeepsBaseline(t *testing.T) {
	fake := newFakeClient("host: base\n")
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

	fake.trigger("host: [broken\n")

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
	fake.trigger("host: base\n")
	assertNoNotification(t, notifications, 300*time.Millisecond)
}

func TestWatchStopIsIdempotent(t *testing.T) {
	fake := newFakeClient("host: base\n")
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

	if count := fake.countCalls("cancel"); count != 1 {
		t.Errorf("cancel calls = %d, want 1", count)
	}
	if count := fake.countCalls("close"); count != 1 {
		t.Errorf("close calls = %d, want 1", count)
	}
	if fake.hasListener() {
		t.Error("listener is still registered after stop")
	}
}

func TestWatchContextCancelStopsQuietly(t *testing.T) {
	fake := newFakeClient("host: base\n")
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
		t.Error("listener is still registered after the context was cancelled")
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
				fake := newFakeClient("")
				fake.getErr = errors.New("boom")

				return fake
			}(),
			wantMsg: "boom",
		},
		{
			name:    "unparsable content",
			fake:    newFakeClient("host: [broken\n"),
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
			if count := tt.fake.countCalls("cancel"); count != 1 {
				t.Errorf("cancel calls = %d, want 1", count)
			}
		})
	}
}

func TestWatchRejectsNilNotify(t *testing.T) {
	fake := newFakeClient("host: base\n")
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
	fake := newFakeClient("host: base\n")
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

func TestWatchStopReportsCancelError(t *testing.T) {
	fake := newFakeClient("host: base\n")
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
