// Package audience 提供受众定义、查询解析和成员评估。
package audience

import "time"

// Audience 代表一个命名受众群体，持有 JSON 条件树。
type Audience struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description,omitempty"`
	Conditions     Condition `json:"conditions"`
	Type           string    `json:"type"` // dynamic / static
	SizeEstimate   int       `json:"size_estimate"`
	LastComputedAt time.Time `json:"last_computed_at,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Condition 是受众查询语法的节点，支持嵌套 and/or。
// 叶节点样例：{"trait":"is_vip","op":"=","value":true}
// 分支样例：{"and":[...]}  {"or":[...]}
type Condition struct {
	// 分支节点
	And []Condition `json:"and,omitempty"`
	Or  []Condition `json:"or,omitempty"`

	// 叶节点 - trait
	Trait string `json:"trait,omitempty"`
	// 叶节点 - attribute
	Attr string `json:"attr,omitempty"`
	// 叶节点 - tag
	Tag string `json:"tag,omitempty"`
	// 叶节点 - event aggregation
	Event      string `json:"event,omitempty"`
	WindowDays int    `json:"window_days,omitempty"`

	// 通用比较
	Op    string `json:"op,omitempty"` // =, !=, >, <, >=, <=, in, not_in, count
	Value any    `json:"value,omitempty"`
	Gte   *int   `json:"gte,omitempty"`
	Lte   *int   `json:"lte,omitempty"`
}

// Member 是受众成员记录。
type Member struct {
	AudienceID  int64     `json:"audience_id"`
	CanonicalID string    `json:"canonical_id"`
	AddedAt     time.Time `json:"added_at"`
}
