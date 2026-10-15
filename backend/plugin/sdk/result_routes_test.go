package sdk

import (
	"context"
	"encoding/json"
	"gorm.io/gorm"
	"testing"
)

func TestResultRouteBelongsToItsDeclaredPage(t *testing.T) {
	for _, tc := range []struct {
		key, action string
		scope       ScopeKind
		valid       bool
	}{
		{"tasks", "", ScopeResult, true},
		{"tasks", "ack", ScopeResult, true},
		{"other", "", ScopeResult, false},
		{"", "", ScopeResult, false},
		{"tasks", "unknown", ScopeResult, false},
		{"tasks", "", ScopeSystem, false},
	} {
		collector := registrationCollector{plugin: PluginDescriptor{ID: "test.business"}, resultPages: map[string]ResultPageDescriptor{
			"test.business/tasks": {Actions: []string{"ack"}, Resources: func(json.RawMessage) ([]WorkflowResource, error) { return nil, nil }, ValidateScope: func(context.Context, *gorm.DB, json.RawMessage) error { return nil }},
		}, routes: map[string]registeredRoute{"test": {desc: RouteDescriptor{Scope: tc.scope, ResultPageKey: tc.key, Action: tc.action}}}}
		if err := collector.validateRoutes(); (err == nil) != tc.valid {
			t.Fatalf("route %+v: %v", tc, err)
		}
	}
}
