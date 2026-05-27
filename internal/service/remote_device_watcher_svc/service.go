package remote_device_watcher_svc

import (
	"context"
	"sync"
)

type watcherEntry struct {
	w      *Watcher
	cancel context.CancelFunc
}

type service struct {
	repo     PairedAgentredReader
	dial     DaemonDialPort
	keychain KeychainPort
	emit     Emitter
	cfg      WatcherConfig
	clock    Clock
	recorder ProviderRecorder // 可空

	mu          sync.Mutex
	watchers    map[int64]watcherEntry
	perDeviceMu sync.Map // map[int64]*sync.Mutex
}

// New 构造 Service。生产由 bootstrap 装配。
// recorder 可为 nil；非 nil 时每次心跳成功后调 RecordDeviceProviders。
func New(
	repo PairedAgentredReader,
	dial DaemonDialPort,
	kc KeychainPort,
	emit Emitter,
	cfg WatcherConfig,
	clock Clock,
	recorder ProviderRecorder,
) Service {
	return &service{
		repo: repo, dial: dial, keychain: kc, emit: emit, cfg: cfg, clock: clock,
		recorder: recorder,
		watchers: map[int64]watcherEntry{},
	}
}

// deviceLock 取（必要时创建）id 的 per-device mutex。
// Stop/Start/Restart 同一 id 串行，避免 Restart 中间窗口被并发 Stop 抢入。
func (s *service) deviceLock(id int64) *sync.Mutex {
	actual, _ := s.perDeviceMu.LoadOrStore(id, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func (s *service) StartAll(ctx context.Context) error {
	rows, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if !row.IsActive() {
			continue
		}
		// best-effort: 某一台启失败不阻断其它(Start 当前实现永远 nil)。
		_ = s.Start(ctx, row.ID)
	}
	return nil
}

func (s *service) Start(ctx context.Context, deviceID int64) error {
	_ = ctx // watcher 用独立 ctx，生命周期跟 svc 而非请求
	perDev := s.deviceLock(deviceID)
	perDev.Lock()
	defer perDev.Unlock()
	return s.startLocked(deviceID)
}

// startLocked 假设 caller 已持 perDevice lock，内部只拿 registry mu。
func (s *service) startLocked(deviceID int64) error {
	s.mu.Lock()
	if _, ok := s.watchers[deviceID]; ok {
		s.mu.Unlock()
		return nil // idempotent
	}
	// watcher 用独立 ctx，与请求 ctx 解绑；生命周期跟随 svc。
	// cancel 存入 s.watchers[deviceID].cancel，在 Stop() 中调用。
	wctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(deviceID, s.repo, s.dial, s.keychain, s.emit, s.cfg, s.clock, s.recorder)
	s.watchers[deviceID] = watcherEntry{w: w, cancel: cancel}
	s.mu.Unlock()
	go w.Run(wctx)
	return nil
}

func (s *service) Stop(deviceID int64) {
	perDev := s.deviceLock(deviceID)
	perDev.Lock()
	defer perDev.Unlock()

	s.mu.Lock()
	e, ok := s.watchers[deviceID]
	delete(s.watchers, deviceID)
	s.mu.Unlock()
	if !ok {
		return
	}
	e.cancel()
	e.w.Wait()
}

func (s *service) Restart(ctx context.Context, deviceID int64) error {
	_ = ctx // watcher 用独立 ctx，生命周期跟 svc 而非请求
	perDev := s.deviceLock(deviceID)
	perDev.Lock()
	defer perDev.Unlock()

	// 内联 Stop（不重入 per-device lock，sync.Mutex 不可重入）。
	s.mu.Lock()
	e, ok := s.watchers[deviceID]
	delete(s.watchers, deviceID)
	s.mu.Unlock()
	if ok {
		e.cancel()
		e.w.Wait()
	}
	return s.startLocked(deviceID)
}

func (s *service) StopAll() {
	s.mu.Lock()
	ids := make([]int64, 0, len(s.watchers))
	for id := range s.watchers {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.Stop(id)
	}
}
