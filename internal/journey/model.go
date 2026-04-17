// Package journey 提供状态机驱动的用户旅程编排。
package journey

import "time"

// Journey 是一个可复用的旅程模板，通过 Steps DAG 描述流程。
type Journey struct {
	ID             int64         `json:"id"`
	Name           string        `json:"name"`
	EntryCondition *EntryTrigger `json:"entry_condition,omitempty"`
	Steps          []Step        `json:"steps"`
	Status         string        `json:"status"` // draft / active / paused / archived
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// EntryTrigger 描述触发用户进入 Journey 的条件。
type EntryTrigger struct {
	// EventType 进入触发：当用户产生该事件时进入
	EventType string `json:"event_type,omitempty"`
	// AudienceID 受众触发：加入某受众时进入
	AudienceID *int64 `json:"audience_id,omitempty"`
	// ScheduleCron 定时触发
	ScheduleCron string `json:"schedule_cron,omitempty"`
}

// Step 是旅程中的一个节点，可以是 action 或 wait。
type Step struct {
	ID      string   `json:"id"`   // 节点唯一 ID（DAG 节点名）
	Type    string   `json:"type"` // action / wait / condition
	Next    []string `json:"next,omitempty"`
	Action  *Action  `json:"action,omitempty"`
	Wait    *Wait    `json:"wait,omitempty"`
	Branch  *Branch  `json:"branch,omitempty"`
}

// Action 描述执行动作。
type Action struct {
	Kind    string         `json:"kind"` // tag / webhook / message
	Payload map[string]any `json:"payload"`
}

// Wait 描述等待条件。
type Wait struct {
	DurationSecs int    `json:"duration_secs,omitempty"`
	UntilEvent   string `json:"until_event,omitempty"`
}

// Branch 描述条件分支。
type Branch struct {
	Conditions []BranchCase `json:"conditions"`
	DefaultTo  string       `json:"default_to"`
}

// BranchCase 是单个分支条件。
type BranchCase struct {
	EventType string `json:"event_type,omitempty"`
	NextStepID string `json:"next_step_id"`
}

// Instance 是某用户在某 Journey 中的执行状态。
type Instance struct {
	ID          int64          `json:"id"`
	JourneyID   int64          `json:"journey_id"`
	CanonicalID string         `json:"canonical_id"`
	CurrentStep string         `json:"current_step"`
	State       map[string]any `json:"state"`
	StartedAt   time.Time      `json:"started_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"`
}
