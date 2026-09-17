package cleannacos

import (
	"sync"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// fakeClient is an in-memory configClient used by the unit tests.
type fakeClient struct {
	mu sync.Mutex

	content   string
	getErr    error
	listenErr error
	cancelErr error

	closed       bool
	onChange     func(namespace, group, dataId, data string)
	calls        []string
	lastGetParam vo.ConfigParam
	lastListen   vo.ConfigParam
}

func newFakeClient(content string) *fakeClient {
	return &fakeClient{content: content}
}

func (f *fakeClient) GetConfig(param vo.ConfigParam) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "get")
	f.lastGetParam = param

	if f.getErr != nil {
		return "", f.getErr
	}

	return f.content, nil
}

func (f *fakeClient) ListenConfig(param vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "listen")
	f.lastListen = param

	if f.listenErr != nil {
		return f.listenErr
	}
	f.onChange = param.OnChange

	return nil
}

func (f *fakeClient) CancelListenConfig(param vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "cancel")

	if f.cancelErr != nil {
		return f.cancelErr
	}
	f.onChange = nil

	return nil
}

// CloseClient makes the fake satisfy the closer interface used by the package.
func (f *fakeClient) CloseClient() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "close")
	f.closed = true
}

// setContent replaces the content served by GetConfig.
func (f *fakeClient) setContent(content string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.content = content
}

// trigger simulates a Nacos change event and reports whether a listener was
// registered to receive it.
func (f *fakeClient) trigger(content string) bool {
	f.mu.Lock()
	onChange := f.onChange
	f.content = content
	f.mu.Unlock()

	if onChange == nil {
		return false
	}
	onChange("", "", "", content)

	return true
}

func (f *fakeClient) isClosed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.closed
}

func (f *fakeClient) hasListener() bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.onChange != nil
}

func (f *fakeClient) callOrder() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]string(nil), f.calls...)
}

func (f *fakeClient) countCalls(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	count := 0
	for _, call := range f.calls {
		if call == name {
			count++
		}
	}

	return count
}

// installFakeClient makes ReadConfig, UpdateConfig and Watch use fake.
func installFakeClient(t *testing.T, fake *fakeClient) {
	t.Helper()

	previous := newClient
	newClient = func(*dsn) (configClient, error) { return fake, nil }
	t.Cleanup(func() { newClient = previous })
}

// waitForNotification waits for the next watch callback.
func waitForNotification[T any](t *testing.T, ch <-chan *T, timeout time.Duration) *T {
	t.Helper()

	select {
	case conf := <-ch:
		return conf
	case <-time.After(timeout):
		t.Fatal("timed out waiting for a watch notification")

		return nil
	}
}

// assertNoNotification fails when a watch callback arrives within wait.
func assertNoNotification[T any](t *testing.T, ch <-chan *T, wait time.Duration) {
	t.Helper()

	select {
	case conf := <-ch:
		t.Fatalf("unexpected watch notification: %+v", conf)
	case <-time.After(wait):
	}
}

// waitForCondition polls condition until it holds or the timeout expires.
func waitForCondition(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("timed out waiting for condition")
}
