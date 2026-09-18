package cleannacos

import (
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/nacos-group/nacos-sdk-go/v2/vo"
)

// fakeSource is the identity the fake client keys its content and listeners
// by: the namespace belongs to the client, the rest to the config parameter.
func fakeSource(namespace string, param vo.ConfigParam) string {
	return namespace + "\x00" + param.Group + "\x00" + param.DataId
}

// fakeClient is an in-memory Nacos client used by the unit tests. One instance
// is shared by every namespace, so tests can count the calls across the pool.
type fakeClient struct {
	mu sync.Mutex

	contents  map[string]string
	listeners map[string]func(namespace, group, dataId, data string)

	getErr    error
	listenErr error
	cancelErr error

	closed bool

	calls   []string
	gets    []vo.ConfigParam
	listens []vo.ConfigParam
	cancels []vo.ConfigParam
}

// newFakeClient returns an empty fake client.
func newFakeClient() *fakeClient {
	return &fakeClient{
		contents:  make(map[string]string),
		listeners: make(map[string]func(namespace, group, dataId, data string)),
	}
}

// setContent stores the content of a dataId of the default group and the
// public namespace.
func (f *fakeClient) setContent(dataID, content string) {
	f.setContentIn("", defaultGroup, dataID, content)
}

// setContentIn stores the content of one source.
func (f *fakeClient) setContentIn(namespace, group, dataID, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.contents[fakeSource(namespace, vo.ConfigParam{DataId: dataID, Group: group})] = content
}

func (f *fakeClient) getConfig(namespace string, param vo.ConfigParam) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "get")
	f.gets = append(f.gets, param)

	if f.getErr != nil {
		return "", f.getErr
	}

	return f.contents[fakeSource(namespace, param)], nil
}

func (f *fakeClient) listenConfig(namespace string, param vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "listen")
	f.listens = append(f.listens, param)

	if f.listenErr != nil {
		return f.listenErr
	}
	f.listeners[fakeSource(namespace, param)] = param.OnChange

	return nil
}

func (f *fakeClient) cancelListenConfig(namespace string, param vo.ConfigParam) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, "cancel")
	f.cancels = append(f.cancels, param)

	if f.cancelErr != nil {
		return f.cancelErr
	}
	delete(f.listeners, fakeSource(namespace, param))

	return nil
}

// trigger simulates a change of one source and reports whether a listener was
// registered for it.
func (f *fakeClient) trigger(dataID, content string) bool {
	return f.triggerIn("", defaultGroup, dataID, content)
}

// triggerIn simulates a change of one source.
func (f *fakeClient) triggerIn(namespace, group, dataID, content string) bool {
	key := fakeSource(namespace, vo.ConfigParam{DataId: dataID, Group: group})

	f.mu.Lock()
	onChange := f.listeners[key]
	f.contents[key] = content
	f.mu.Unlock()

	if onChange == nil {
		return false
	}
	onChange(namespace, group, dataID, content)

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

	return len(f.listeners) > 0
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

// getDataIDs returns the sorted dataIds the fake served.
func (f *fakeClient) getDataIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(f.gets))
	for _, param := range f.gets {
		ids = append(ids, param.DataId)
	}
	sort.Strings(ids)

	return ids
}

// listenDataIDs returns the sorted dataIds with a registered listener.
func (f *fakeClient) listenDataIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(f.listens))
	for _, param := range f.listens {
		ids = append(ids, param.DataId)
	}
	sort.Strings(ids)

	return ids
}

// cancelDataIDs returns the sorted dataIds whose listener was cancelled.
func (f *fakeClient) cancelDataIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	ids := make([]string, 0, len(f.cancels))
	for _, param := range f.cancels {
		ids = append(ids, param.DataId)
	}
	sort.Strings(ids)

	return ids
}

// lastGet returns the config parameter of the last fetch.
func (f *fakeClient) lastGet() vo.ConfigParam {
	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.gets) == 0 {
		return vo.ConfigParam{}
	}

	return f.gets[len(f.gets)-1]
}

// fakeClientView binds the shared fake to one namespace, mirroring the real
// client, which carries its namespace in the client configuration.
type fakeClientView struct {
	fake      *fakeClient
	namespace string
}

func (v *fakeClientView) GetConfig(param vo.ConfigParam) (string, error) {
	return v.fake.getConfig(v.namespace, param)
}

func (v *fakeClientView) ListenConfig(param vo.ConfigParam) error {
	return v.fake.listenConfig(v.namespace, param)
}

func (v *fakeClientView) CancelListenConfig(param vo.ConfigParam) error {
	return v.fake.cancelListenConfig(v.namespace, param)
}

// CloseClient makes the view satisfy the closer interface used by the package.
func (v *fakeClientView) CloseClient() {
	v.fake.mu.Lock()
	defer v.fake.mu.Unlock()

	v.fake.calls = append(v.fake.calls, "close")
	v.fake.closed = true
}

// installFakeClient makes ReadConfig, UpdateConfig and Watch use fake.
func installFakeClient(t *testing.T, fake *fakeClient) {
	t.Helper()

	previous := newClient
	newClient = func(_ *dsn, namespace string) (configClient, error) {
		return &fakeClientView{fake: fake, namespace: namespace}, nil
	}
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
