package service

import (
	"coinsphere/backend/internal/db"
	"context"
	"fmt"
	"gorm.io/gorm"
	"time"
)

type principalKey struct{}

func WithPrincipal(ctx context.Context, p *Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}
func ContextPrincipal(ctx context.Context) *Principal {
	p, _ := ctx.Value(principalKey{}).(*Principal)
	return p
}
func requireAccount(ctx context.Context) error {
	if !ContextPrincipal(ctx).IsOwner() {
		return ErrPermission
	}
	return nil
}
func workflowScopeQuery(query *gorm.DB, p *Principal) *gorm.DB {
	if !p.IsOwner() {
		return query.Where("FALSE")
	}
	return query
}
func (a *App) AuthorizeWorkflow(ctx context.Context, id int64) error {
	p, err := principalForTx(a.DB.WithContext(ctx), ContextPrincipal(ctx))
	if err != nil {
		return err
	}
	return authorizeWorkflowTx(a.DB.WithContext(ctx), p, id)
}
func (a *App) authorizeRun(ctx context.Context, id int64) error {
	var run db.WorkflowRun
	if err := a.DB.WithContext(ctx).Select("workflow_id").First(&run, id).Error; err != nil {
		return ErrNotFound
	}
	return a.AuthorizeWorkflow(ctx, run.WorkflowID)
}
func (a *App) authorizeExecution(userID int64, _ validatedWorkflowGraph) error {
	_, err := a.buildPrincipal(userID)
	return err
}
func (a *App) RevalidateSession(p *Principal) (*Principal, error) {
	if !p.IsOwner() || p.AccessTokenID == "" {
		return nil, ErrPermission
	}
	return principalForTx(a.DB, p)
}
func principalForTx(tx *gorm.DB, p *Principal) (*Principal, error) {
	if p == nil || p.User == nil || p.User.ID <= 0 {
		return nil, ErrPermission
	}
	if p.AccessTokenID != "" {
		if !p.AccessTokenExp.After(time.Now().UTC()) {
			return nil, ErrPermission
		}
		var revoked int64
		if err := tx.Model(&db.RevokedSession{}).Where("token_id=?", p.AccessTokenID).Count(&revoked).Error; err != nil {
			return nil, err
		}
		if revoked != 0 {
			return nil, ErrPermission
		}
	}
	var user db.SystemUser
	if err := tx.Where("id=? AND username='coinsphere' AND is_active", p.User.ID).First(&user).Error; err != nil {
		return nil, ErrPermission
	}
	if p.AccessTokenID != "" && user.SessionVersion != p.User.SessionVersion {
		return nil, ErrPermission
	}
	next := *p
	next.User = &user
	return &next, nil
}

// Commands, logout and password changes serialize before taking resource row locks.
func lockAuthorization(tx *gorm.DB) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended('coinsphere.authorization',0))").Error
}
func commandPrincipalTx(tx *gorm.DB, ctx context.Context) (*Principal, error) {
	if err := lockAuthorization(tx); err != nil {
		return nil, err
	}
	return principalForTx(tx, ContextPrincipal(ctx))
}
func (a *App) authorizeWorkflowCommandTx(tx *gorm.DB, ctx context.Context, id int64) (*Principal, error) {
	p, err := commandPrincipalTx(tx, ctx)
	if err != nil {
		return nil, err
	}
	return p, authorizeWorkflowTx(tx, p, id)
}
func authorizeWorkflowTx(tx *gorm.DB, p *Principal, id int64) error {
	if !p.IsOwner() {
		return ErrPermission
	}
	var count int64
	if err := tx.Model(&db.Workflow{}).Where("id=?", id).Count(&count).Error; err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("%w: workflow", ErrNotFound)
	}
	return nil
}

func personalPrincipalTx(tx *gorm.DB) (*Principal, error) {
	var user db.SystemUser
	if err := tx.Where("username='coinsphere' AND is_active").First(&user).Error; err != nil {
		return nil, ErrPermission
	}
	return &Principal{User: &user}, nil
}
