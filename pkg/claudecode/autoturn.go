package claudecode

// AutoTurn 是 CLI 在没有新 user 输入的情况下自主跑的一轮 —— 当前唯一来源是
// run_in_background Bash 任务完成后,CLI 自主注入 <task-notification> 并跑完整
// 一轮(列目录之类)。Events 与普通 Turn 的事件流同形:result 帧到达后 close。
//
// 消费方(agentruntime claudecode runtime)从 Session.AutonomousTurns() 收到本值,
// drain Events 翻译成 agentruntime 事件,再由 chat_svc 落成一条纯 assistant 轮。
type AutoTurn struct {
	Events    <-chan Event
	SessionID string
	Trigger   string // 当前固定 "background_task"
	// CompletedTask 仅 background_task 触发的自主轮带:那个完成的后台任务的身份,
	// 供上层把对应 subagent_state 翻成 completed/failed。
	CompletedTask *CompletedBackgroundTask
}

// CompletedBackgroundTask 是触发本自主轮的后台命令完成信息(从 task_notification 帧抽出)。
type CompletedBackgroundTask struct {
	ToolUseID string
	TaskID    string
	Status    string // "completed" / "failed"(空 → 视为 completed)
	Summary   string // CLI task_notification.summary,如 "Background command \"…\" completed (exit code 0)"
}

// triggerBackgroundTask 是目前唯一的 AutoTurn 触发原因。
const triggerBackgroundTask = "background_task"

// activeTurn 是 readLoop 当前正在投递帧的那一轮 —— 可能是用户发起的 Turn,也可能
// 是自主轮。一刻只有一个(CLI 串行 emit 各轮,每轮以 result 收尾,从不交错)。
type activeTurn struct {
	ch         chan Event    // 投递事件给消费方;result/EOF 时由 readLoop close
	done       chan struct{} // readLoop 在本轮收尾(result/EOF)时 close,唤醒 Turn 的 waiter
	abandon    chan struct{} // Turn 的 waiter 在 ctx 取消时 close;readLoop 据此停止投递、丢弃余帧
	autonomous bool          // 自主轮 = true(经 AutonomousTurns 吐出,无对应 Turn 调用)
}

// newActiveTurn 造一轮的投递三件套。ch 带缓冲削峰(单一消费方实时 drain)。
func newActiveTurn(autonomous bool) *activeTurn {
	return &activeTurn{
		ch:         make(chan Event, 16),
		done:       make(chan struct{}),
		abandon:    make(chan struct{}),
		autonomous: autonomous,
	}
}

// isBackgroundTaskNotification 判定一帧是否为「后台命令完成」通知 —— 它是自主续轮
// 的起始标记,与 subagent(Task 工具)的 task_notification 区分:
//   - 后台型:有 output_file(落在 tasks/<id>.output),无 subagent_type。
//   - subagent 型:有 subagent_type / description,无 output_file。
func isBackgroundTaskNotification(f rawFrame) bool {
	return f.Type == "system" &&
		f.Subtype == "task_notification" &&
		f.OutputFile != "" &&
		f.SubagentType == ""
}
