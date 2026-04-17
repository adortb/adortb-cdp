package audience

import (
	"encoding/json"
	"fmt"
)

// ParseCondition 将 JSON 字节解析为 Condition 树。
func ParseCondition(data []byte) (Condition, error) {
	if len(data) == 0 {
		return Condition{}, fmt.Errorf("empty condition")
	}
	var c Condition
	if err := json.Unmarshal(data, &c); err != nil {
		return Condition{}, fmt.Errorf("parse condition: %w", err)
	}
	if err := validateCondition(c, 0); err != nil {
		return Condition{}, err
	}
	return c, nil
}

const maxDepth = 10

func validateCondition(c Condition, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("condition nesting too deep (max %d)", maxDepth)
	}

	isBranch := len(c.And) > 0 || len(c.Or) > 0
	isLeaf := c.Trait != "" || c.Attr != "" || c.Tag != "" || c.Event != ""

	if isBranch && isLeaf {
		return fmt.Errorf("condition cannot be both branch and leaf")
	}
	if !isBranch && !isLeaf {
		return fmt.Errorf("condition must have at least one clause")
	}

	for _, child := range c.And {
		if err := validateCondition(child, depth+1); err != nil {
			return err
		}
	}
	for _, child := range c.Or {
		if err := validateCondition(child, depth+1); err != nil {
			return err
		}
	}

	if isLeaf && c.Tag == "" && c.Op == "" {
		return fmt.Errorf("leaf condition requires op field")
	}

	return nil
}
