package service

import (
	"encoding/json"
	"testing"
)

func TestWorkflowSchemaFieldBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		schema string
		path   []string
		valid  bool
		typ    string
	}{
		{"dynamic model output", `{"type":"object","properties":{"data":{"type":"object"}},"additionalProperties":false}`, []string{"data", "message"}, true, ""},
		{"nested dynamic output", `{"type":"object"}`, []string{"data", "message"}, true, ""},
		{"explicit dynamic output", `{"type":"object","additionalProperties":true}`, []string{"message"}, true, ""},
		{"typed map", `{"type":"object","additionalProperties":{"type":"string"}}`, []string{"message"}, true, "string"},
		{"closed object", `{"type":"object","additionalProperties":false}`, []string{"message"}, false, ""},
		{"scalar has no fields", `{"type":"string"}`, []string{"message"}, false, ""},
		{"typed map values have no fields", `{"type":"object","additionalProperties":{"type":"string"}}`, []string{"message", "value"}, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			field, valid := workflowSchemaField(json.RawMessage(tc.schema), tc.path)
			if valid != tc.valid || tc.typ != "" && field["type"] != tc.typ {
				t.Fatalf("field=%v valid=%v", field, valid)
			}
		})
	}
}
