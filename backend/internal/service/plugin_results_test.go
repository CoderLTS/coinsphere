package service

import (
	"coinsphere/backend/internal/db"
	"fmt"
	"gorm.io/gorm"
	"net/url"
	"testing"
	"time"
)

func TestDirectResultsResourcesSessionActionsAndNoSavedViews(t *testing.T) {
	a, ctx, p := platformFixture(t, nil)
	w := createTestWorkflow(t, a, ctx, p, testGraph("task"))
	query := url.Values{"workflowId": {fmt.Sprint(w.ID)}, "nodeId": {"task"}}
	scope, err := a.ResolveDirectResultScope(ctx, "test.business", "tasks", "ack", query, p)
	if err != nil || len(scope.Resources) != 1 || scope.Revalidate == nil {
		t.Fatal(scope, err)
	}
	if err := a.DB.Transaction(func(tx *gorm.DB) error { return scope.Revalidate(ctx, tx) }); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []url.Values{{"workflowId": {"999999"}, "nodeId": {"task"}}, {"workflowId": {fmt.Sprint(w.ID)}, "nodeId": {"end"}}, {"workflowId": {"1", "2"}}, {"scope": {"{}"}}, {"userId": {"1"}}, {"workflowId": {"wrong"}}} {
		if _, err := a.ResolveDirectResultScope(ctx, "test.business", "tasks", "", invalid, p); err == nil {
			t.Fatal("invalid direct scope accepted", invalid)
		}
	}
	if _, err := a.ResolveDirectResultScope(ctx, "test.business", "missing", "", query, p); err == nil {
		t.Fatal("missing page accepted")
	}
	if _, err := a.ResolveDirectResultScope(ctx, "test.business", "tasks", "undeclared", query, p); err == nil {
		t.Fatal("undeclared action accepted")
	}
	if _, err := a.ResolveDirectResultScope(ctx, "test.business", "tasks", "", query, nil); err == nil {
		t.Fatal("anonymous result scope accepted")
	}
	if a.DB.Migrator().HasTable("result_views") {
		t.Fatal("direct read created a saved view")
	}
	p.AccessTokenID = "synthetic-result-session"
	p.AccessTokenExp = time.Now().UTC().Add(time.Hour)
	if err := a.DB.Model(&db.SystemUser{}).Where("id=?", p.User.ID).Update("session_version", gorm.Expr("session_version+1")).Error; err != nil {
		t.Fatal(err)
	}
	if err := a.DB.Transaction(func(tx *gorm.DB) error { return scope.Revalidate(ctx, tx) }); err == nil {
		t.Fatal("write accepted stale session")
	}
	if _, err := a.ResolveDirectResultScope(ctx, "test.business", "tasks", "", query, p); err == nil {
		t.Fatal("old session read results")
	}
}
