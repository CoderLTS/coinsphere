package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"coinsphere/backend/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const maxWorkflowGroupAssignments = 1000

type WorkflowGroupUpsertPayload struct {
	Name string `json:"name"`
}

type WorkflowGroupOrderPayload struct {
	GroupIDs []int64 `json:"groupIds"`
}

type WorkflowGroupAssignmentPayload struct {
	WorkflowIDs []int64 `json:"workflowIds"`
	GroupID     *int64  `json:"groupId"`
}

type WorkflowGroupView struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sortOrder"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type WorkflowGroupAssignmentResult struct {
	Updated int64 `json:"updated"`
}

func (a *App) ListWorkflowGroups(ctx context.Context) ([]WorkflowGroupView, error) {
	if err := requireAccount(ctx); err != nil {
		return nil, err
	}
	var groups []db.WorkflowGroup
	if err := a.DB.WithContext(ctx).Order("sort_order, id").Find(&groups).Error; err != nil {
		return nil, errors.New("读取工作流分组失败")
	}
	result := make([]WorkflowGroupView, len(groups))
	for index := range groups {
		result[index] = workflowGroupView(groups[index])
	}
	return result, nil
}

func (a *App) CreateWorkflowGroup(ctx context.Context, payload WorkflowGroupUpsertPayload) (WorkflowGroupView, error) {
	if err := requireAccount(ctx); err != nil {
		return WorkflowGroupView{}, err
	}
	now := time.Now().UTC()
	group := db.WorkflowGroup{CreatedAt: now, UpdatedAt: now}
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE workflow_groups IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return errors.New("锁定工作流分组失败")
		}
		name, err := validateWorkflowGroupName(tx, payload.Name, 0)
		if err != nil {
			return err
		}
		group.Name = name
		if err := tx.Model(&db.WorkflowGroup{}).Select("COALESCE(MAX(sort_order), -1) + 1").Scan(&group.SortOrder).Error; err != nil {
			return errors.New("读取工作流分组顺序失败")
		}
		if err := tx.Create(&group).Error; err != nil {
			return errors.New("创建工作流分组失败")
		}
		return nil
	})
	if err != nil {
		return WorkflowGroupView{}, err
	}
	return workflowGroupView(group), nil
}

func (a *App) UpdateWorkflowGroup(ctx context.Context, groupID int64, payload WorkflowGroupUpsertPayload) (WorkflowGroupView, error) {
	if err := requireAccount(ctx); err != nil {
		return WorkflowGroupView{}, err
	}
	var group db.WorkflowGroup
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE workflow_groups IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return errors.New("锁定工作流分组失败")
		}
		if err := tx.First(&group, groupID).Error; err != nil {
			return workflowGroupLookupError(err)
		}
		name, err := validateWorkflowGroupName(tx, payload.Name, groupID)
		if err != nil {
			return err
		}
		group.Name = name
		group.UpdatedAt = time.Now().UTC()
		if err := tx.Model(&group).Updates(map[string]any{
			"name": group.Name, "updated_at": group.UpdatedAt,
		}).Error; err != nil {
			return errors.New("更新工作流分组失败")
		}
		return nil
	})
	if err != nil {
		return WorkflowGroupView{}, err
	}
	return workflowGroupView(group), nil
}

func (a *App) DeleteWorkflowGroup(ctx context.Context, groupID int64) error {
	if err := requireAccount(ctx); err != nil {
		return err
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		result := tx.Delete(&db.WorkflowGroup{}, groupID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrNotFound
		}
		return nil
	})
}

func (a *App) UpdateWorkflowGroupOrder(ctx context.Context, payload WorkflowGroupOrderPayload) ([]WorkflowGroupView, error) {
	if err := requireAccount(ctx); err != nil {
		return nil, err
	}
	groupIDs := uniquePositiveInt64s(payload.GroupIDs)
	if len(groupIDs) != len(payload.GroupIDs) {
		return nil, errors.New("分组编号须为不重复的正整数")
	}
	now := time.Now().UTC()
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := commandPrincipalTx(tx, ctx); err != nil {
			return err
		}
		if err := tx.Exec("LOCK TABLE workflow_groups IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return errors.New("锁定工作流分组失败")
		}
		var currentIDs []int64
		if err := tx.Model(&db.WorkflowGroup{}).Order("id").Pluck("id", &currentIDs).Error; err != nil {
			return errors.New("读取工作流分组失败")
		}
		if len(currentIDs) != len(groupIDs) {
			return fmt.Errorf("%w：工作流分组已变更，请刷新后重试", ErrConflict)
		}
		requested := make(map[int64]bool, len(groupIDs))
		for _, groupID := range groupIDs {
			requested[groupID] = true
		}
		for _, groupID := range currentIDs {
			if !requested[groupID] {
				return fmt.Errorf("%w：工作流分组已变更，请刷新后重试", ErrConflict)
			}
		}
		for index, groupID := range payload.GroupIDs {
			if err := tx.Model(&db.WorkflowGroup{}).Where("id = ?", groupID).Updates(map[string]any{
				"sort_order": index, "updated_at": now,
			}).Error; err != nil {
				return errors.New("更新工作流分组顺序失败")
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return a.ListWorkflowGroups(ctx)
}

func (a *App) AssignWorkflowGroup(ctx context.Context, payload WorkflowGroupAssignmentPayload) (WorkflowGroupAssignmentResult, error) {
	for _, id := range payload.WorkflowIDs {
		if err := a.AuthorizeWorkflow(ctx, id); err != nil {
			return WorkflowGroupAssignmentResult{}, err
		}
	}
	workflowIDs := uniquePositiveInt64s(payload.WorkflowIDs)
	if len(workflowIDs) == 0 || len(workflowIDs) != len(payload.WorkflowIDs) {
		return WorkflowGroupAssignmentResult{}, errors.New("工作流编号须为不重复的正整数")
	}
	if len(workflowIDs) > maxWorkflowGroupAssignments {
		return WorkflowGroupAssignmentResult{}, fmt.Errorf("一次最多调整 %d 个工作流的分组", maxWorkflowGroupAssignments)
	}
	result := WorkflowGroupAssignmentResult{}
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		p, err := commandPrincipalTx(tx, ctx)
		if err != nil {
			return err
		}
		for _, id := range workflowIDs {
			if err := authorizeWorkflowTx(tx, p, id); err != nil {
				return err
			}
		}
		if err := validateWorkflowGroupID(tx, payload.GroupID); err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&db.Workflow{}).Where("id IN ?", workflowIDs).Count(&count).Error; err != nil {
			return errors.New("读取待调整分组的工作流失败")
		}
		if count != int64(len(workflowIDs)) {
			return fmt.Errorf("%w：工作流", ErrNotFound)
		}
		var groupID any
		if payload.GroupID != nil {
			groupID = *payload.GroupID
		}
		update := tx.Model(&db.Workflow{}).Where("id IN ?", workflowIDs).Updates(map[string]any{
			"group_id": groupID, "updated_at": time.Now().UTC(),
		})
		if update.Error != nil {
			return errors.New("调整工作流分组失败")
		}
		result.Updated = update.RowsAffected
		return nil
	})
	return result, err
}

func validateWorkflowGroupName(database *gorm.DB, value string, currentID int64) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return "", errors.New("工作流分组名称须为 1 至 80 个字符")
	}
	query := database.Model(&db.WorkflowGroup{}).Where("LOWER(name) = LOWER(?)", name)
	if currentID > 0 {
		query = query.Where("id <> ?", currentID)
	}
	var duplicate int64
	if err := query.Count(&duplicate).Error; err != nil {
		return "", errors.New("检查工作流分组名称失败")
	}
	if duplicate > 0 {
		return "", fmt.Errorf("%w：工作流分组名称已存在", ErrConflict)
	}
	return name, nil
}

func validateWorkflowGroupID(database *gorm.DB, groupID *int64) error {
	if groupID == nil {
		return nil
	}
	if *groupID <= 0 {
		return errors.New("分组编号须为正整数或空值")
	}
	var group db.WorkflowGroup
	if err := database.Clauses(clause.Locking{Strength: "KEY SHARE"}).Select("id").First(&group, *groupID).Error; err != nil {
		return workflowGroupLookupError(err)
	}
	return nil
}

func workflowGroupLookupError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("%w：工作流分组", ErrNotFound)
	}
	return errors.New("读取工作流分组失败")
}

func workflowGroupView(group db.WorkflowGroup) WorkflowGroupView {
	return WorkflowGroupView{
		ID: group.ID, Name: group.Name, SortOrder: group.SortOrder,
		CreatedAt: formatWorkflowTime(group.CreatedAt), UpdatedAt: formatWorkflowTime(group.UpdatedAt),
	}
}
