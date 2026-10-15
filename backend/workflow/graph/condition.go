package graph

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"github.com/shopspring/decimal"
)

var ConditionConfigSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"match":{"type":"string","title":"组合方式","enum":["all","any"],"default":"all"},"rules":{"type":"array","title":"判断规则","minItems":1,"maxItems":64,"default":[{"fieldPath":["value"],"operator":"true"}],"items":{"type":"object","properties":{"fieldPath":{"type":"array","items":{"type":"string","minLength":1,"maxLength":128},"minItems":1,"maxItems":12},"operator":{"type":"string","enum":["eq","ne","gt","gte","lt","lte","contains","true","false","exists"],"title":"运算符"},"value":{"title":"值"}},"required":["fieldPath","operator"],"additionalProperties":false}}},"required":["match","rules"],"additionalProperties":false}`)
var ConditionInputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","additionalProperties":{}}`)
var ConditionOutputSchema = json.RawMessage(`{"$schema":"https://json-schema.org/draft/2020-12/schema","type":"object","properties":{"branch":{"type":"string","enum":["true","false","unavailable"],"title":"分支"},"available":{"type":"boolean","title":"是否可用"},"matched":{"type":["boolean","null"],"title":"是否匹配"}},"required":["branch","available","matched"],"additionalProperties":false}`)

type conditionConfig struct {
	Match string `json:"match"`
	Rules []struct {
		FieldPath []string        `json:"fieldPath"`
		Operator  string          `json:"operator"`
		Value     json.RawMessage `json:"value"`
	} `json:"rules"`
}

func ValidateCondition(raw json.RawMessage) error {
	var config conditionConfig
	if json.Unmarshal(raw, &config) != nil || (config.Match != "all" && config.Match != "any") || len(config.Rules) < 1 || len(config.Rules) > 64 {
		return errors.New("condition requires all/any and 1 to 64 rules")
	}
	for _, rule := range config.Rules {
		if len(rule.FieldPath) < 1 || len(rule.FieldPath) > 12 {
			return errors.New("condition field path is invalid")
		}
		for _, part := range rule.FieldPath {
			if strings.TrimSpace(part) == "" || len(part) > 128 {
				return errors.New("condition field path is invalid")
			}
		}
		switch rule.Operator {
		case "true", "false", "exists":
		case "eq", "ne", "gt", "gte", "lt", "lte", "contains":
			if !json.Valid(rule.Value) || bytes.Equal(bytes.TrimSpace(rule.Value), []byte("null")) {
				return errors.New("condition comparison value is required")
			}
		default:
			return errors.New("condition operator is invalid")
		}
	}
	return nil
}

// EvaluateCondition is pure; platform runs and plugin frame execution share its routing semantics.
func EvaluateCondition(raw json.RawMessage, input map[string]any) (map[string]any, error) {
	if err := ValidateCondition(raw); err != nil {
		return nil, err
	}
	var config conditionConfig
	_ = json.Unmarshal(raw, &config)
	matched := config.Match == "all"
	available := true
	for _, rule := range config.Rules {
		value, exists := Field(input, rule.FieldPath)
		if !exists || value == nil {
			available = false
			continue
		}
		var expected any
		if len(rule.Value) > 0 {
			decoder := json.NewDecoder(bytes.NewReader(rule.Value))
			decoder.UseNumber()
			if err := decoder.Decode(&expected); err != nil {
				return nil, err
			}
		}
		result, valid := compareCondition(value, expected, rule.Operator)
		available = available && valid
		// Evaluate every rule: a decisive Boolean never masks unavailable data.
		if config.Match == "all" {
			matched = matched && result
		} else {
			matched = matched || result
		}
	}
	if !available {
		return map[string]any{"branch": "unavailable", "available": false, "matched": nil}, nil
	}
	branch := "false"
	if matched {
		branch = "true"
	}
	return map[string]any{"branch": branch, "available": true, "matched": matched}, nil
}

func compareCondition(value, expected any, operator string) (bool, bool) {
	switch operator {
	case "exists":
		return true, true
	case "true", "false":
		boolean, ok := value.(bool)
		return boolean == (operator == "true"), ok
	case "contains":
		text, ok := value.(string)
		needle, valid := expected.(string)
		return strings.Contains(text, needle), ok && valid
	case "eq", "ne":
		leftKind, rightKind := conditionValueKind(value), conditionValueKind(expected)
		if leftKind == "unknown" || leftKind != rightKind {
			return false, false
		}
		matched := reflect.DeepEqual(value, expected)
		if leftKind == "number" {
			left, e1 := decimal.NewFromString(fmt.Sprint(value))
			right, e2 := decimal.NewFromString(fmt.Sprint(expected))
			if e1 != nil || e2 != nil {
				return false, false
			}
			matched = left.Equal(right)
		}
		if operator == "ne" {
			matched = !matched
		}
		return matched, true
	default:
		if conditionValueKind(value) != "number" && conditionValueKind(value) != "string" || conditionValueKind(expected) != "number" && conditionValueKind(expected) != "string" {
			return false, false
		}
		left, e1 := decimal.NewFromString(fmt.Sprint(value))
		right, e2 := decimal.NewFromString(fmt.Sprint(expected))
		if e1 != nil || e2 != nil {
			return false, false
		}
		cmp := left.Cmp(right)
		switch operator {
		case "gt":
			return cmp > 0, true
		case "gte":
			return cmp >= 0, true
		case "lt":
			return cmp < 0, true
		case "lte":
			return cmp <= 0, true
		}
	}
	return false, false
}

func conditionValueKind(value any) string {
	switch value.(type) {
	case json.Number, float64, int, int64:
		return "number"
	case string:
		return "string"
	case bool:
		return "boolean"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "unknown"
	}
}
