package cc_usage_svc

// 单例:App.Startup 在 wire-up 时调 SetEmitter / SetFetcherResolver / StartTicker,
// wails binding 通过 CCUsage() 拿到同一个 Manager 调 Get / Probe。
// 与其它 svc 一样 SetDefault 由 bootstrap 注入(便于单测换实现);
// 默认值是一个零依赖空 Manager,保证未注入也不 panic(Probe 会 no-op)。
var defaultMgr = NewManager(ManagerOpts{})

// CCUsage 返回当前进程的单例 Manager。
func CCUsage() *Manager { return defaultMgr }

// SetDefault 由 bootstrap 注入。生产路径几乎不调(默认值已够用),
// 单测或 swap 实现时使用。
func SetDefault(m *Manager) {
	if m == nil {
		return
	}
	defaultMgr = m
}
