package remote_device_svc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	repomock "github.com/agentre-ai/agentre/internal/repository/remote_device_repo/mock_remote_device_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	svcmock "github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

// poolFixture 给 Pool 单测装好 mock + 一台 device 的标准数据。
type poolFixture struct {
	t      *testing.T
	ctrl   *gomock.Controller
	repo   *repomock.MockPairedAgentredRepo
	dial   *svcmock.MockDaemonDialPort
	kc     keychain.Keychain
	pool   remote_device_svc.ConnPool
	device *paired_agentred_entity.PairedAgentred
}

func newPoolFixture(t *testing.T, opts ...remote_device_svc.Option) *poolFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := repomock.NewMockPairedAgentredRepo(ctrl)
	dial := svcmock.NewMockDaemonDialPort(ctrl)
	kc := keychain.NewMemory()
	_ = kc.Set("agentre-daemon-token-42", "tok-42")
	_ = kc.Set("agentre-device-fingerprint", "fp-x")
	row := &paired_agentred_entity.PairedAgentred{
		ID: 42, Name: "agentred-a", URL: "wss://example/rpc",
		TLSMode: "skip-verify", DaemonFingerprint: "sha256:abc",
	}
	return &poolFixture{
		t:      t,
		ctrl:   ctrl,
		repo:   repo,
		dial:   dial,
		kc:     kc,
		pool:   remote_device_svc.NewConnPool(repo, kc, dial, opts...),
		device: row,
	}
}

// stubClient 返回一个非 nil 的 *client.Client sentinel。Pool 不应该真的对它
// 调 Call/Close —— 这些行为应由集成测验。单测里 Pool 只持有指针。
func stubClient() *client.Client { return &client.Client{} }

func expectBorrowDialError(
	f *poolFixture,
	dialErr error,
	wantErr error,
) {
	f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil)
	f.dial.EXPECT().Open(gomock.Any(), gomock.Any()).Return(nil, dialErr)
	_, err := f.pool.Borrow(context.Background(), 42)
	So(errors.Is(err, wantErr), ShouldBeTrue)
}

func TestPool_Borrow_DeviceNotFound(t *testing.T) {
	Convey("repo returns nil row → ErrDeviceNotFound", t, func() {
		f := newPoolFixture(t)
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(nil, nil)
		_, err := f.pool.Borrow(context.Background(), 42)
		So(errors.Is(err, remote_device_svc.ErrDeviceNotFound), ShouldBeTrue)
	})
}

func TestPool_Borrow_KeychainMissingToken(t *testing.T) {
	Convey("keychain missing token → ErrDeviceUnauthorized", t, func() {
		f := newPoolFixture(t)
		_ = f.kc.Delete("agentre-daemon-token-42")
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil)
		_, err := f.pool.Borrow(context.Background(), 42)
		So(errors.Is(err, remote_device_svc.ErrDeviceUnauthorized), ShouldBeTrue)
	})
}

func TestPool_Borrow_DialUnauthorizedMapped(t *testing.T) {
	Convey("dial returns ErrUnauthorized → ErrDeviceUnauthorized", t, func() {
		f := newPoolFixture(t)
		expectBorrowDialError(f, remote_device_svc.ErrUnauthorized, remote_device_svc.ErrDeviceUnauthorized)
	})
}

func TestPool_Borrow_DialTOFUMismatchPassthrough(t *testing.T) {
	Convey("dial returns ErrTOFUMismatch → propagated", t, func() {
		f := newPoolFixture(t)
		expectBorrowDialError(f, remote_device_svc.ErrTOFUMismatch, remote_device_svc.ErrTOFUMismatch)
	})
}

func TestPool_Release_RecycleBeforeTimeout(t *testing.T) {
	Convey("Borrow during idle window cancels timer and reuses entry", t, func() {
		f := newPoolFixture(t, remote_device_svc.WithIdleTimeout(200*time.Millisecond))
		c := stubClient()
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil).Times(1)
		f.dial.EXPECT().Open(gomock.Any(), gomock.Any()).Return(c, nil).Times(1)

		l1, err := f.pool.Borrow(context.Background(), 42)
		So(err, ShouldBeNil)
		l1.Release()
		// 30ms 后(远小于 idle 200ms)再 Borrow
		time.Sleep(30 * time.Millisecond)
		l2, err := f.pool.Borrow(context.Background(), 42)
		So(err, ShouldBeNil)
		So(l2.Client(), ShouldEqual, l1.Client())

		// 静等过 idleTimeout 总长 —— 因 l2 还在,不应 evict
		time.Sleep(250 * time.Millisecond)
		select {
		case <-l2.Closed():
			t.Fatal("entry evicted even though it was re-borrowed")
		default:
		}
	})
}

func TestPool_Release_EvictsAfterIdleTimeout(t *testing.T) {
	Convey("Release w/ no other borrowers evicts entry after idle timeout", t, func() {
		f := newPoolFixture(t, remote_device_svc.WithIdleTimeout(20*time.Millisecond))
		c := stubClient()
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil).Times(1)
		f.dial.EXPECT().Open(gomock.Any(), gomock.Any()).Return(c, nil).Times(1)

		l, err := f.pool.Borrow(context.Background(), 42)
		So(err, ShouldBeNil)
		l.Release()

		// idle 到点后 Lease.Closed() 应关闭
		select {
		case <-l.Closed():
		case <-time.After(200 * time.Millisecond):
			t.Fatal("entry not evicted within 200ms after idle")
		}
	})
}

func TestPool_Borrow_TOCTOU_ConcurrentColdStart(t *testing.T) {
	Convey("Concurrent first-borrows resolve to a single entry", t, func() {
		f := newPoolFixture(t)
		var openCount int32
		clients := []*client.Client{stubClient(), stubClient(), stubClient(), stubClient(), stubClient()}
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).
			Return(f.device, nil).AnyTimes()
		f.dial.EXPECT().
			Open(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ remote_device_svc.ConnectArgs) (*client.Client, error) {
				i := atomic.AddInt32(&openCount, 1) - 1
				return clients[i], nil
			}).
			AnyTimes()

		const N = 5
		var wg sync.WaitGroup
		leases := make([]remote_device_svc.Lease, N)
		errs := make([]error, N)
		wg.Add(N)
		for i := 0; i < N; i++ {
			go func() {
				defer wg.Done()
				leases[i], errs[i] = f.pool.Borrow(context.Background(), 42)
			}()
		}
		wg.Wait()
		for i, err := range errs {
			So(err, ShouldBeNil)
			So(leases[i], ShouldNotBeNil)
		}
		first := leases[0].Client()
		for i := 1; i < N; i++ {
			So(leases[i].Client(), ShouldEqual, first)
		}
	})
}

func TestPool_Borrow_FastPath_ReusesEntry(t *testing.T) {
	Convey("Second Borrow on same device reuses entry, no dial", t, func() {
		f := newPoolFixture(t)
		c := stubClient()
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil).Times(1)
		f.dial.EXPECT().Open(gomock.Any(), gomock.Any()).Return(c, nil).Times(1)

		l1, err := f.pool.Borrow(context.Background(), 42)
		So(err, ShouldBeNil)
		l2, err := f.pool.Borrow(context.Background(), 42)
		So(err, ShouldBeNil)

		So(l1.Client(), ShouldEqual, l2.Client())
	})
}

func TestPool_Borrow_ColdStart(t *testing.T) {
	Convey("Borrow on a fresh device dials and returns a lease", t, func() {
		f := newPoolFixture(t)
		c := stubClient()
		f.repo.EXPECT().Get(gomock.Any(), int64(42)).Return(f.device, nil)
		f.dial.EXPECT().
			Open(gomock.Any(), gomock.Any()).
			Return(c, nil).
			Times(1)

		lease, err := f.pool.Borrow(context.Background(), 42)

		So(err, ShouldBeNil)
		So(lease, ShouldNotBeNil)
		So(lease.Client(), ShouldNotBeNil)
		// Closed channel should be open at this point.
		select {
		case <-lease.Closed():
			t.Fatal("Closed() fired before any drop / Release")
		default:
		}
		assert.NotNil(t, c)
	})
}
