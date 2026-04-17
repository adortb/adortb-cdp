package audience

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/adortb/adortb-cdp/internal/profile"
)

// EventCounter 提供按类型统计事件数量的能力（由 ingestion 层实现）。
type EventCounter interface {
	CountEvents(ctx context.Context, canonicalID, eventType string, since time.Time) (int, error)
}

// Evaluator 判断某个 profile 是否满足受众条件。
type Evaluator struct {
	counter EventCounter
}

// NewEvaluator 构造 Evaluator。
func NewEvaluator(counter EventCounter) *Evaluator {
	return &Evaluator{counter: counter}
}

// Matches 返回 profile 是否满足条件树。
func (e *Evaluator) Matches(ctx context.Context, p *profile.Profile, cond Condition) (bool, error) {
	// 分支节点 AND
	if len(cond.And) > 0 {
		for _, c := range cond.And {
			ok, err := e.Matches(ctx, p, c)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil
	}

	// 分支节点 OR
	if len(cond.Or) > 0 {
		for _, c := range cond.Or {
			ok, err := e.Matches(ctx, p, c)
			if err != nil {
				return false, err
			}
			if ok {
				return true, nil
			}
		}
		return false, nil
	}

	// 叶节点：tag
	if cond.Tag != "" {
		return containsTag(p.Tags, cond.Tag), nil
	}

	// 叶节点：trait
	if cond.Trait != "" {
		val, ok := p.Traits[cond.Trait]
		if !ok {
			return false, nil
		}
		return compare(val, cond.Op, cond.Value)
	}

	// 叶节点：attribute
	if cond.Attr != "" {
		val, ok := p.Attributes[cond.Attr]
		if !ok {
			return false, nil
		}
		return compare(val, cond.Op, cond.Value)
	}

	// 叶节点：event count
	if cond.Event != "" {
		if e.counter == nil {
			return false, fmt.Errorf("event counter not configured")
		}
		windowDays := cond.WindowDays
		if windowDays <= 0 {
			windowDays = 30
		}
		since := time.Now().UTC().AddDate(0, 0, -windowDays)
		cnt, err := e.counter.CountEvents(ctx, p.CanonicalID, cond.Event, since)
		if err != nil {
			return false, err
		}
		if cond.Gte != nil && cnt < *cond.Gte {
			return false, nil
		}
		if cond.Lte != nil && cnt > *cond.Lte {
			return false, nil
		}
		return true, nil
	}

	return false, fmt.Errorf("unrecognized condition node: %+v", cond)
}

func containsTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// compare 实现通用比较，支持 =、!=、>、<、>=、<=、in、not_in。
func compare(actual any, op string, expected any) (bool, error) {
	switch strings.ToLower(op) {
	case "=", "eq":
		return reflect.DeepEqual(actual, expected), nil
	case "!=", "ne", "neq":
		return !reflect.DeepEqual(actual, expected), nil
	case "in":
		return containsValue(expected, actual), nil
	case "not_in":
		return !containsValue(expected, actual), nil
	case ">", "gt":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a > b })
	case "<", "lt":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a < b })
	case ">=", "gte":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a >= b })
	case "<=", "lte":
		return compareNumeric(actual, expected, func(a, b float64) bool { return a <= b })
	default:
		return false, fmt.Errorf("unknown op: %s", op)
	}
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case json_number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// json_number alias 避免循环导入 encoding/json。
type json_number interface{ Float64() (float64, error) }

func compareNumeric(actual, expected any, fn func(a, b float64) bool) (bool, error) {
	a, ok1 := toFloat(actual)
	b, ok2 := toFloat(expected)
	if !ok1 || !ok2 {
		return false, fmt.Errorf("non-numeric comparison: %T vs %T", actual, expected)
	}
	return fn(a, b), nil
}

func containsValue(list any, val any) bool {
	v := reflect.ValueOf(list)
	if v.Kind() != reflect.Slice {
		return false
	}
	for i := range v.Len() {
		if reflect.DeepEqual(v.Index(i).Interface(), val) {
			return true
		}
	}
	return false
}
