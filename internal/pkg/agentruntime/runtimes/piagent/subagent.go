package piagent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/canonical"
)

type subagentMode string

const (
	subagentModeSingle   subagentMode = "single"
	subagentModeParallel subagentMode = "parallel"
	subagentModeChain    subagentMode = "chain"
)

type envelopeKind string

const (
	envelopePending  envelopeKind = "single-envelope-pending"
	envelopeOfficial envelopeKind = "official"
	envelopeFlat     envelopeKind = "flat-single"
)

type invocationRun struct {
	ID             string
	Index          int
	Agent          string
	Profile        string
	Task           string
	RequestedModel string
}

type subagentInvocation struct {
	Mode                  subagentMode
	Runs                  []invocationRun
	Envelope              envelopeKind
	AgentScope            string
	ConfirmProjectAgents  bool
	projectConfirmEnabled bool
}

type subagentSelector struct {
	classify   func([]byte) (subagentInvocation, bool)
	newTracker func(string, subagentInvocation) *subagentTracker
}

var defaultSubagentSelector = subagentSelector{
	classify:   classifySubagentInvocation,
	newTracker: newSubagentTracker,
}

func (s subagentSelector) selectCandidate(name, outerToolCallID string, input []byte) (*subagentTracker, *canonical.AgentSpawn) {
	if !strings.Contains(strings.ToLower(name), "subagent") {
		return nil, nil
	}
	inv, ok := s.classify(input)
	if !ok || inv.Mode != subagentModeSingle {
		return nil, nil
	}
	tracker := s.newTracker(outerToolCallID, inv)
	spawn := tracker.spawn()
	return tracker, &spawn
}

func classifySubagentInvocation(input []byte) (subagentInvocation, bool) {
	var fields map[string]json.RawMessage
	if len(bytes.TrimSpace(input)) == 0 || json.Unmarshal(input, &fields) != nil || fields == nil {
		return subagentInvocation{}, false
	}

	agent, agentPresent, ok := readOptionalString(fields, "agent")
	if !ok {
		return subagentInvocation{}, false
	}
	task, taskPresent, ok := readOptionalString(fields, "task")
	if !ok {
		return subagentInvocation{}, false
	}
	profile, profilePresent, ok := readOptionalString(fields, "profile")
	if !ok {
		return subagentInvocation{}, false
	}
	model, _, ok := readOptionalString(fields, "model")
	if !ok {
		return subagentInvocation{}, false
	}
	if _, _, ok = readOptionalString(fields, "thinking"); !ok {
		return subagentInvocation{}, false
	}
	if _, _, ok = readOptionalString(fields, "cwd"); !ok {
		return subagentInvocation{}, false
	}

	tasks, tasksPresent, ok := readInvocationRuns(fields, "tasks", 8)
	if !ok {
		return subagentInvocation{}, false
	}
	chain, chainPresent, ok := readInvocationRuns(fields, "chain", 0)
	if !ok {
		return subagentInvocation{}, false
	}

	agentScope, scopePresent, ok := readOptionalString(fields, "agentScope")
	if !ok {
		return subagentInvocation{}, false
	}
	if scopePresent && agentScope != "user" && agentScope != "project" && agentScope != "both" {
		return subagentInvocation{}, false
	}
	confirmProjectAgents := true
	confirmPresent := false
	if raw, exists := fields["confirmProjectAgents"]; exists {
		confirmPresent = true
		if err := json.Unmarshal(raw, &confirmProjectAgents); err != nil {
			return subagentInvocation{}, false
		}
	}

	agent = strings.TrimSpace(agent)
	task = strings.TrimSpace(task)
	profile = strings.TrimSpace(profile)
	model = strings.TrimSpace(model)
	agentActive := agentPresent && taskPresent && agent != "" && task != ""
	parallelActive := tasksPresent && len(tasks) > 0
	chainActive := chainPresent && len(chain) > 0
	activeOfficial := 0
	for _, active := range []bool{agentActive, parallelActive, chainActive} {
		if active {
			activeOfficial++
		}
	}
	if activeOfficial > 1 {
		return subagentInvocation{}, false
	}

	inv := subagentInvocation{
		AgentScope:            agentScope,
		ConfirmProjectAgents:  confirmProjectAgents,
		projectConfirmEnabled: (agentScope == "project" || agentScope == "both") && (!confirmPresent || confirmProjectAgents),
	}
	switch {
	case agentActive:
		inv.Mode = subagentModeSingle
		inv.Envelope = envelopePending
		inv.Runs = []invocationRun{{ID: "run-0", Agent: agent, Task: task, RequestedModel: model}}
	case parallelActive:
		inv.Mode = subagentModeParallel
		inv.Envelope = envelopeOfficial
		inv.Runs = tasks
	case chainActive:
		inv.Mode = subagentModeChain
		inv.Envelope = envelopeOfficial
		inv.Runs = chain
	case taskPresent && profilePresent && task != "" && profile != "":
		inv.Mode = subagentModeSingle
		inv.Envelope = envelopeFlat
		inv.Runs = []invocationRun{{ID: "run-0", Profile: profile, Task: task, RequestedModel: model}}
	default:
		return subagentInvocation{}, false
	}
	return inv, true
}

func readOptionalString(fields map[string]json.RawMessage, key string) (string, bool, bool) {
	raw, exists := fields[key]
	if !exists {
		return "", false, true
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", true, false
	}
	return value, true, true
}

func readInvocationRuns(fields map[string]json.RawMessage, key string, limit int) ([]invocationRun, bool, bool) {
	raw, exists := fields[key]
	if !exists {
		return nil, false, true
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil || items == nil {
		return nil, true, false
	}
	if limit > 0 && len(items) > limit {
		return nil, true, false
	}
	runs := make([]invocationRun, 0, len(items))
	for index, item := range items {
		var entry map[string]json.RawMessage
		if json.Unmarshal(item, &entry) != nil || entry == nil {
			return nil, true, false
		}
		agent, presentAgent, ok := readOptionalString(entry, "agent")
		if !ok {
			return nil, true, false
		}
		task, presentTask, ok := readOptionalString(entry, "task")
		if !ok {
			return nil, true, false
		}
		if _, _, cwdOK := readOptionalString(entry, "cwd"); !cwdOK {
			return nil, true, false
		}
		agent = strings.TrimSpace(agent)
		task = strings.TrimSpace(task)
		if !presentAgent || !presentTask || agent == "" || task == "" {
			return nil, true, false
		}
		runs = append(runs, invocationRun{ID: fmt.Sprintf("run-%d", index), Index: index, Agent: agent, Task: task})
	}
	return runs, true, true
}

type trackedRun struct {
	info              agentruntime.SubagentRun
	emittedCalls      map[string]string
	emittedResults    map[string]bool
	pendingResults    map[string]trackedResult
	lastAssistantText string
}

type trackedResult struct {
	content string
	isError bool
}

type subagentTracker struct {
	outerToolCallID string
	invocation      subagentInvocation
	envelope        envelopeKind
	runs            []trackedRun
	activity        bool
}

func newSubagentTracker(outerToolCallID string, inv subagentInvocation) *subagentTracker {
	t := &subagentTracker{outerToolCallID: outerToolCallID, invocation: inv, envelope: inv.Envelope}
	t.runs = make([]trackedRun, len(inv.Runs))
	for i, run := range inv.Runs {
		status := "running"
		if inv.Mode != subagentModeSingle {
			status = "waiting"
		}
		t.runs[i] = trackedRun{
			info: agentruntime.SubagentRun{
				ID: run.ID, Index: run.Index, Agent: run.Agent, Profile: run.Profile,
				Task: run.Task, RequestedModel: run.RequestedModel, Status: status,
			},
			emittedCalls:   make(map[string]string),
			emittedResults: make(map[string]bool),
			pendingResults: make(map[string]trackedResult),
		}
	}
	return t
}

func (t *subagentTracker) spawn() canonical.AgentSpawn {
	runs := make([]canonical.AgentSpawnRun, len(t.invocation.Runs))
	for i, run := range t.invocation.Runs {
		runs[i] = canonical.AgentSpawnRun{
			ID: run.ID, Index: run.Index, Agent: run.Agent, Profile: run.Profile,
			Task: run.Task, RequestedModel: run.RequestedModel,
		}
	}
	first := t.invocation.Runs[0]
	return canonical.AgentSpawn{
		SubagentType:    first.Agent,
		TaskDescription: first.Task,
		Prompt:          first.Task,
		Model:           first.RequestedModel,
		Mode:            string(t.invocation.Mode),
		Runs:            runs,
		Status:          "running",
	}
}

func (t *subagentTracker) info() agentruntime.SubagentInfo {
	runs := make([]agentruntime.SubagentRun, len(t.runs))
	toolUses := 0
	lastToolName := ""
	for i := range t.runs {
		runs[i] = t.runs[i].info
		toolUses += runs[i].ToolUses
		if runs[i].LastToolName != "" {
			lastToolName = runs[i].LastToolName
		}
	}
	first := runs[0]
	return agentruntime.SubagentInfo{
		SubagentType:    first.Agent,
		TaskDescription: first.Task,
		Prompt:          first.Task,
		LastToolName:    lastToolName,
		ToolUses:        toolUses,
		Status:          aggregateSingleStatus(runs),
		Mode:            string(t.invocation.Mode),
		Runs:            runs,
	}
}

func aggregateSingleStatus(runs []agentruntime.SubagentRun) string {
	if len(runs) == 0 {
		return "unknown"
	}
	if len(runs) == 1 {
		return runs[0].Status
	}
	for _, run := range runs {
		if run.Status == "running" || run.Status == "waiting" {
			return "running"
		}
		if run.Status == "failed" {
			return "failed"
		}
	}
	return "unknown"
}

func (t *subagentTracker) consumeUpdate(partialResult []byte) ([]agentruntime.Event, bool) {
	details, ok := unwrapPartialDetails(partialResult)
	if !ok {
		return nil, false
	}
	return t.consumeDetails(details, false, false)
}

func (t *subagentTracker) consumeFinal(details []byte, outerError bool, _ string) ([]agentruntime.Event, bool) {
	return t.consumeDetails(details, true, outerError)
}

func unwrapPartialDetails(partial []byte) ([]byte, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(partial, &object) != nil || object == nil {
		return nil, false
	}
	details, ok := object["details"]
	return details, ok
}

type decodedSnapshot struct {
	messages      []json.RawMessage
	exitCode      *int
	stopReason    string
	status        string
	model         string
	agentSource   string
	errorMessage  string
	summary       string
	emptyOfficial bool
}

func (t *subagentTracker) consumeDetails(details []byte, final, outerError bool) ([]agentruntime.Event, bool) {
	before := t.info()
	snapshots, usable := t.decode(details)
	events := make([]agentruntime.Event, 0)
	for index, snapshot := range snapshots {
		if index >= len(t.runs) {
			break
		}
		events = append(events, t.applySnapshot(index, snapshot, final)...)
	}

	if final {
		t.finalize(snapshots, usable, outerError)
	}
	after := t.info()
	return events, !reflect.DeepEqual(before, after)
}

func (t *subagentTracker) decode(details []byte) ([]decodedSnapshot, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(details, &object) != nil || object == nil {
		return nil, false
	}

	resultsRaw, resultsPresent := object["results"]
	messagesRaw, messagesPresent := object["messages"]
	results, resultsOK := rawArray(resultsRaw)
	messages, messagesOK := rawArray(messagesRaw)
	mode, modePresent, modeOK := readSnapshotMode(object)

	if t.envelope == envelopePending {
		switch {
		case resultsPresent && !messagesPresent && resultsOK && modeOK && (!modePresent || mode == "single"):
			t.envelope = envelopeOfficial
		case messagesPresent && !resultsPresent && messagesOK:
			t.envelope = envelopeFlat
		default:
			return nil, false
		}
	}

	switch t.envelope {
	case envelopeOfficial:
		if !resultsOK || !modeOK || modePresent && mode != string(t.invocation.Mode) {
			return nil, false
		}
		if len(results) == 0 {
			return []decodedSnapshot{{emptyOfficial: true}}, true
		}
		out := make([]decodedSnapshot, 0, len(results))
		for _, raw := range results {
			out = append(out, decodeSnapshotObject(raw, "messages"))
		}
		return out, true
	case envelopeFlat:
		if !messagesOK {
			return nil, false
		}
		snapshot := decodeSnapshotMap(object, messages)
		return []decodedSnapshot{snapshot}, true
	default:
		return nil, false
	}
}

func readSnapshotMode(object map[string]json.RawMessage) (string, bool, bool) {
	raw, exists := object["mode"]
	if !exists {
		return "", false, true
	}
	var mode string
	if json.Unmarshal(raw, &mode) != nil {
		return "", true, false
	}
	return mode, true, true
}

func rawArray(raw json.RawMessage) ([]json.RawMessage, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) != nil || values == nil {
		return nil, false
	}
	return values, true
}

func decodeSnapshotObject(raw json.RawMessage, messagesKey string) decodedSnapshot {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return decodedSnapshot{}
	}
	messages, _ := rawArray(object[messagesKey])
	return decodeSnapshotMap(object, messages)
}

func decodeSnapshotMap(object map[string]json.RawMessage, messages []json.RawMessage) decodedSnapshot {
	snapshot := decodedSnapshot{messages: messages}
	snapshot.exitCode = readOptionalInt(object["exitCode"])
	snapshot.stopReason = readTrimmedString(object["stopReason"])
	snapshot.status = readTrimmedString(object["status"])
	snapshot.model = readTrimmedString(object["model"])
	snapshot.agentSource = readTrimmedString(object["agentSource"])
	snapshot.errorMessage = firstNonEmpty(
		readTrimmedString(object["errorMessage"]),
		readTrimmedString(object["error"]),
	)
	snapshot.summary = firstNonEmpty(
		readTrimmedString(object["summary"]),
		readTrimmedString(object["output"]),
	)
	return snapshot
}

func readOptionalInt(raw json.RawMessage) *int {
	if len(raw) == 0 {
		return nil
	}
	var value int
	if json.Unmarshal(raw, &value) != nil {
		return nil
	}
	return &value
}

func readTrimmedString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (t *subagentTracker) applySnapshot(index int, snapshot decodedSnapshot, final bool) []agentruntime.Event {
	run := &t.runs[index]
	if !snapshot.emptyOfficial {
		t.activity = true
	}
	if snapshot.agentSource != "" && run.info.AgentSource == "" {
		run.info.AgentSource = snapshot.agentSource
	}

	var events []agentruntime.Event
	for _, rawMessage := range snapshot.messages {
		messageEvents, activity := t.applyMessage(run, rawMessage, final)
		if activity {
			t.activity = true
		}
		events = append(events, messageEvents...)
	}
	if snapshot.model != "" && run.info.Model == "" {
		run.info.Model = snapshot.model
	}
	t.applyStopReason(run, snapshot.stopReason)
	if snapshot.status == "failed" || snapshot.status == "canceled" {
		run.info.Status = snapshot.status
	}
	if snapshot.errorMessage != "" {
		run.info.ErrorMessage = snapshot.errorMessage
		run.info.Status = "failed"
	}
	if final {
		if snapshot.summary != "" {
			run.info.Summary = snapshot.summary
		} else if run.lastAssistantText != "" {
			run.info.Summary = run.lastAssistantText
		}
	}
	return events
}

func (t *subagentTracker) applyMessage(run *trackedRun, raw json.RawMessage, final bool) ([]agentruntime.Event, bool) {
	var message map[string]json.RawMessage
	if json.Unmarshal(raw, &message) != nil || message == nil {
		return nil, false
	}
	role := readTrimmedString(message["role"])
	switch role {
	case "assistant":
		observedModel := readTrimmedString(message["model"])
		if observedModel != "" && run.info.Model == "" {
			run.info.Model = observedModel
		}
		stopReason := readTrimmedString(message["stopReason"])
		errorMessage := readTrimmedString(message["errorMessage"])
		if errorMessage != "" {
			run.info.ErrorMessage = errorMessage
		}
		content, ok := rawArray(message["content"])
		if !ok {
			content = nil
		}
		var events []agentruntime.Event
		var texts []string
		for _, rawContent := range content {
			var item map[string]json.RawMessage
			if json.Unmarshal(rawContent, &item) != nil || item == nil {
				continue
			}
			switch readTrimmedString(item["type"]) {
			case "toolCall":
				id := readTrimmedString(item["id"])
				name := readTrimmedString(item["name"])
				if id == "" || name == "" {
					continue
				}
				input := objectRaw(item["arguments"])
				callEvents := t.emitCall(run, id, name, input)
				events = append(events, callEvents...)
			case "text":
				if text := readRawString(item["text"]); text != "" {
					texts = append(texts, text)
				}
			}
		}
		if len(texts) > 0 {
			run.lastAssistantText = strings.Join(texts, "")
			if final {
				run.info.Summary = run.lastAssistantText
			}
		}
		t.applyStopReason(run, stopReason)
		if errorMessage != "" {
			run.info.Status = "failed"
		}
		return events, len(content) > 0 || stopReason != "" || observedModel != "" || errorMessage != ""
	case "toolResult":
		id := readTrimmedString(message["toolCallId"])
		if id == "" {
			return nil, false
		}
		content, valid := toolResultContent(message["content"])
		if !valid {
			return nil, false
		}
		var isError bool
		_ = json.Unmarshal(message["isError"], &isError)
		return t.emitOrHoldResult(run, id, trackedResult{content: content, isError: isError}), true
	default:
		return nil, false
	}
}

func readRawString(raw json.RawMessage) string {
	var value string
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value
}

func objectRaw(raw json.RawMessage) json.RawMessage {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil
	}
	return append(json.RawMessage(nil), raw...)
}

func toolResultContent(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, true
	}
	items, ok := rawArray(raw)
	if !ok {
		return "", false
	}
	parts := make([]string, 0, len(items))
	for _, rawItem := range items {
		var item map[string]json.RawMessage
		if json.Unmarshal(rawItem, &item) != nil || item == nil || readTrimmedString(item["type"]) != "text" {
			continue
		}
		parts = append(parts, readRawString(item["text"]))
	}
	return strings.Join(parts, ""), true
}

func (t *subagentTracker) emitCall(run *trackedRun, innerID, name string, input json.RawMessage) []agentruntime.Event {
	if _, exists := run.emittedCalls[innerID]; exists {
		return nil
	}
	runtimeID := nestedToolCallID(t.outerToolCallID, run.info.ID, innerID)
	run.emittedCalls[innerID] = runtimeID
	run.info.ToolUses++
	run.info.LastToolName = name
	call := agentruntime.ToolCall{
		ID: runtimeID, Name: name, Input: input,
		Canonical:        recognizeCanonical(name, input),
		ParentToolCallID: t.outerToolCallID, SubagentRunID: run.info.ID,
	}
	events := []agentruntime.Event{call}
	if pending, ok := run.pendingResults[innerID]; ok {
		delete(run.pendingResults, innerID)
		events = append(events, t.emitResult(run, innerID, pending)...)
	}
	return events
}

func (t *subagentTracker) emitOrHoldResult(run *trackedRun, innerID string, result trackedResult) []agentruntime.Event {
	if run.emittedResults[innerID] {
		return nil
	}
	if _, exists := run.emittedCalls[innerID]; !exists {
		run.pendingResults[innerID] = result
		return nil
	}
	return t.emitResult(run, innerID, result)
}

func (t *subagentTracker) emitResult(run *trackedRun, innerID string, result trackedResult) []agentruntime.Event {
	if run.emittedResults[innerID] {
		return nil
	}
	run.emittedResults[innerID] = true
	return []agentruntime.Event{agentruntime.ToolResult{
		ToolCallID: run.emittedCalls[innerID], Content: result.content, IsError: result.isError,
		ParentToolCallID: t.outerToolCallID, SubagentRunID: run.info.ID,
	}}
}

func nestedToolCallID(outerToolCallID, runID, innerToolCallID string) string {
	return fmt.Sprintf("pi-subagent:%d:%s:%d:%s:%d:%s", len(outerToolCallID), outerToolCallID, len(runID), runID, len(innerToolCallID), innerToolCallID)
}

func (t *subagentTracker) applyStopReason(run *trackedRun, reason string) {
	switch reason {
	case "toolUse":
		if !isTerminalStatus(run.info.Status) {
			run.info.Status = "running"
		}
	case "stop", "length":
		run.info.Status = "completed"
	case "error":
		run.info.Status = "failed"
	case "aborted":
		if t.envelope == envelopeOfficial {
			run.info.Status = "failed"
		} else {
			run.info.Status = "canceled"
		}
	}
}

func (t *subagentTracker) finalize(snapshots []decodedSnapshot, usable, outerError bool) {
	if t.invocation.projectConfirmEnabled && t.envelope == envelopeOfficial && !t.activity && len(snapshots) == 1 && snapshots[0].emptyOfficial {
		for i := range t.runs {
			t.runs[i].info.Status = "canceled"
		}
		return
	}

	for index := range t.runs {
		run := &t.runs[index]
		if !usable {
			if outerError {
				run.info.Status = "failed"
			} else {
				run.info.Status = "completed"
			}
			continue
		}
		if isTerminalStatus(run.info.Status) {
			continue
		}
		var snapshot decodedSnapshot
		if index < len(snapshots) {
			snapshot = snapshots[index]
		}
		if terminal := terminalStatus(t.envelope, snapshot); terminal != "" {
			run.info.Status = terminal
			continue
		}
		if outerError {
			run.info.Status = "failed"
		} else {
			run.info.Status = "unknown"
		}
	}
}

func terminalStatus(envelope envelopeKind, snapshot decodedSnapshot) string {
	switch snapshot.stopReason {
	case "stop", "length":
		return "completed"
	case "error":
		return "failed"
	case "aborted":
		if envelope == envelopeOfficial {
			return "failed"
		}
		return "canceled"
	}
	switch snapshot.status {
	case "completed", "failed", "canceled":
		return snapshot.status
	}
	if snapshot.errorMessage != "" {
		return "failed"
	}
	if snapshot.exitCode != nil {
		switch *snapshot.exitCode {
		case 0:
			return "completed"
		case -1:
			return ""
		default:
			return "failed"
		}
	}
	return ""
}

func isTerminalStatus(status string) bool {
	switch status {
	case "completed", "failed", "canceled", "skipped", "unknown":
		return true
	default:
		return false
	}
}
