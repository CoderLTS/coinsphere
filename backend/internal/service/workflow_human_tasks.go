package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var errWorkflowWaiting = errors.New("工作流正在等待持久信号")

type WorkflowHumanTaskView struct {
	WorkflowName   string `json:"workflowName,omitempty"`
	NodeName       string `json:"nodeName,omitempty"`
	ID             int64  `json:"id"`
	WorkflowID     int64  `json:"workflowId"`
	RunID          int64  `json:"runId"`
	NodeInstanceID string `json:"nodeInstanceId"`
	TaskType       string `json:"taskType"`
	BusinessKey    string `json:"businessKey"`
	Prompt         string `json:"prompt"`
	Status         string `json:"status"`
	ExpiresAt      string `json:"expiresAt"`
	CreatedAt      string `json:"createdAt"`
	DecidedAt      string `json:"decidedAt,omitempty"`
}

type WorkflowHumanTaskDecision struct {
	Action string         `json:"action"`
	Data   map[string]any `json:"data,omitempty"`
}

func (a *App) workflowHumanApproval(ctx context.Context, run db.WorkflowRun, node workflowGraphNode, input json.RawMessage) (sdk.ActionResult, error) {
	var config struct {
		DecisionMode  string `json:"decisionMode"`
		TaskType      string `json:"taskType"`
		Prompt        string `json:"prompt"`
		ExpiresSecond int    `json:"expiresSeconds"`
	}
	var values struct {
		BusinessKey string `json:"businessKey"`
	}
	if json.Unmarshal(node.Config, &config) != nil || json.Unmarshal(input, &values) != nil ||
		strings.TrimSpace(values.BusinessKey) == "" || config.ExpiresSecond < 60 {
		return sdk.ActionResult{}, errors.New("人工审批配置无效")
	}
	now := time.Now().UTC()
	if config.DecisionMode == "auto" {
		return sdk.ActionResult{Output: mustJSON(map[string]any{
			"taskId": 0, "status": "approved", "decidedAt": formatWorkflowTime(now),
		})}, nil
	}
	if config.DecisionMode != "" && config.DecisionMode != "human" {
		return sdk.ActionResult{}, errors.New("人工审批决策模式无效")
	}
	var task db.WorkflowHumanTask
	err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockHumanTaskWorkflow(tx, run.WorkflowID); err != nil {
			return err
		}
		if err := a.lockExecutionLease(tx, run, nil); err != nil {
			return err
		}
		businessKey := strings.TrimSpace(values.BusinessKey)
		identity := fmt.Sprintf("%d:%d:%s:%d:%s", run.WorkflowID, len(node.NodeInstanceID), node.NodeInstanceID, len(businessKey), businessKey)
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, identity).Error; err != nil {
			return errors.New("锁定人工待办标识失败")
		}
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("run_id = ? AND node_instance_id = ?", run.ID, node.NodeInstanceID).First(&task).Error
		if err == nil {
			if task.Status == "pending" && !task.ExpiresAt.After(now) {
				if err := finishWorkflowHumanTask(tx, &task, "expired", nil, nil, now); err != nil {
					return err
				}
			}
			if task.Status == "pending" {
				return setHumanTaskWaiting(tx, run, node.NodeInstanceID, now)
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return errors.New("读取人工待办失败")
		}
		var superseded []db.WorkflowHumanTask
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where(
			"workflow_id = ? AND node_instance_id = ? AND business_key = ? AND status = 'pending'",
			run.WorkflowID, node.NodeInstanceID, businessKey,
		).Find(&superseded).Error; err != nil {
			return errors.New("读取已替代的人工待办失败")
		}
		for index := range superseded {
			if err := finishWorkflowHumanTask(tx, &superseded[index], "superseded", nil, nil, now); err != nil {
				return err
			}
			if err := resumeWorkflowHumanTaskRun(tx, superseded[index].RunID, now); err != nil {
				return err
			}
		}
		task = db.WorkflowHumanTask{
			WorkflowID: run.WorkflowID, RunID: run.ID, NodeInstanceID: node.NodeInstanceID,
			TaskType: strings.TrimSpace(config.TaskType), BusinessKey: businessKey,
			Prompt: strings.TrimSpace(config.Prompt), Status: "pending", ExpiresAt: now.Add(time.Duration(config.ExpiresSecond) * time.Second),
			DecisionJSON: `{}`, CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.Create(&task).Error; err != nil {
			return errors.New("创建人工待办失败")
		}
		return setHumanTaskWaiting(tx, run, node.NodeInstanceID, now)
	})
	if err != nil {
		return sdk.ActionResult{}, err
	}
	if task.Status == "pending" {
		return sdk.ActionResult{}, errWorkflowWaiting
	}
	decidedAt := now
	if task.DecidedAt != nil {
		decidedAt = task.DecidedAt.UTC()
	}
	return sdk.ActionResult{Output: mustJSON(map[string]any{
		"taskId": task.ID, "status": task.Status, "decidedAt": formatWorkflowTime(decidedAt),
	})}, nil
}

type WorkflowHumanTaskQuery struct {
	Page       CursorPage
	Status     string
	WorkflowID int64
	RunID      int64
}

func (a *App) PageWorkflowHumanTasks(ctx context.Context, input WorkflowHumanTaskQuery) (M, error) {
	if err := requireAccount(ctx); err != nil {
		return nil, err
	}
	status := strings.TrimSpace(input.Status)
	if status != "" && status != "pending" && status != "approved" && status != "rejected" && status != "expired" && status != "superseded" {
		return nil, errors.New("人工待办状态无效")
	}
	if input.WorkflowID < 0 || input.RunID < 0 {
		return nil, errors.New("工作流或运行编号无效")
	}
	query := workflowScopeQuery(a.DB.WithContext(ctx).Model(&db.WorkflowHumanTask{}), ContextPrincipal(ctx))
	if status != "" {
		query = query.Where("workflow_human_tasks.status = ?", status)
	}
	if input.WorkflowID > 0 {
		query = query.Where("workflow_human_tasks.workflow_id = ?", input.WorkflowID)
	}
	if input.RunID > 0 {
		query = query.Where("workflow_human_tasks.run_id = ?", input.RunID)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, errors.New("读取人工待办数量失败")
	}
	after, err := input.Page.AfterID()
	if err != nil {
		return nil, err
	}
	if after > 0 {
		query = query.Where("workflow_human_tasks.id < ?", after)
	}
	var rows []struct {
		db.WorkflowHumanTask
		WorkflowName string
		NodeName     string
	}
	err = query.Select(`workflow_human_tasks.*, w.name AS workflow_name, COALESCE((SELECT COALESCE(NULLIF(n->>'label',''),n->>'nodeInstanceId') FROM jsonb_array_elements(rev.graph_json->'nodes') n WHERE n->>'nodeInstanceId'=workflow_human_tasks.node_instance_id LIMIT 1), workflow_human_tasks.node_instance_id) AS node_name`).Joins("JOIN workflows w ON w.id=workflow_human_tasks.workflow_id").Joins("JOIN workflow_runs r ON r.id=workflow_human_tasks.run_id").Joins("JOIN workflow_revisions rev ON rev.id=r.revision_id").Order("workflow_human_tasks.id DESC").Limit(input.Page.Limit + 1).Scan(&rows).Error
	if err != nil {
		return nil, errors.New("读取人工待办失败")
	}
	more := len(rows) > input.Page.Limit
	if more {
		rows = rows[:input.Page.Limit]
	}
	views := make([]WorkflowHumanTaskView, len(rows))
	for i, row := range rows {
		views[i] = workflowHumanTaskView(row.WorkflowHumanTask)
		views[i].WorkflowName, views[i].NodeName = row.WorkflowName, row.NodeName
	}
	key := ""
	if len(rows) > 0 {
		key = int64CursorKey(rows[len(rows)-1].ID)
	}
	return cursorResult(views, input.Page, key, more, total), nil
}

func (a *App) DecideWorkflowHumanTask(ctx context.Context, taskID int64, payload WorkflowHumanTaskDecision, principal *Principal) (WorkflowHumanTaskView, error) {
	if principal == nil || principal.User == nil || principal.User.ID <= 0 {
		return WorkflowHumanTaskView{}, ErrPermission
	}
	action := strings.ToLower(strings.TrimSpace(payload.Action))
	if action != "approve" && action != "reject" {
		return WorkflowHumanTaskView{}, errors.New("人工待办操作须为通过或拒绝")
	}
	if payload.Data == nil {
		payload.Data = map[string]any{}
	}
	decision, err := json.Marshal(payload.Data)
	if err != nil || len(decision) > 64<<10 {
		return WorkflowHumanTaskView{}, errors.New("人工待办决策不能超过 64 KiB")
	}
	now := time.Now().UTC()
	var task db.WorkflowHumanTask
	expired := false
	if err := a.DB.WithContext(ctx).First(&task, taskID).Error; err != nil {
		return WorkflowHumanTaskView{}, ErrNotFound
	}
	if err := a.AuthorizeWorkflow(ctx, task.WorkflowID); err != nil {
		return WorkflowHumanTaskView{}, err
	}
	err = a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := a.authorizeWorkflowCommandTx(tx, ctx, task.WorkflowID)
		if err != nil {
			return err
		}
		principal = current
		if err := lockHumanTaskWorkflow(tx, task.WorkflowID); err != nil {
			return err
		}
		var run db.WorkflowRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&run, task.RunID).Error; err != nil {
			return err
		}
		if run.Status != RunStatusWaiting {
			return ErrConflict
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, taskID).Error; err != nil {
			return err
		}
		if task.Status != "pending" {
			return fmt.Errorf("%w：人工待办已处理，请刷新当前状态", ErrConflict)
		}
		if !task.ExpiresAt.After(now) {
			if err := finishWorkflowHumanTask(tx, &task, "expired", nil, nil, now); err != nil {
				return err
			}
			if err := resumeWorkflowHumanTaskRun(tx, task.RunID, now); err != nil {
				return err
			}
			expired = true
			return nil
		}
		status := "approved"
		if action == "reject" {
			status = "rejected"
		}
		actorID := principal.User.ID
		if err := finishWorkflowHumanTask(tx, &task, status, decision, &actorID, now); err != nil {
			return err
		}
		return resumeWorkflowHumanTaskRun(tx, task.RunID, now)
	})
	if err != nil {
		return WorkflowHumanTaskView{}, err
	}
	if expired {
		return WorkflowHumanTaskView{}, fmt.Errorf("%w：人工待办已过期，请刷新当前状态", ErrConflict)
	}
	a.PublishWorkflowRunUpdated(task.WorkflowID, task.RunID)
	return workflowHumanTaskView(task), nil
}

func (a *App) expireWorkflowHumanTasks(ctx context.Context, now time.Time) error {
	var tasks []db.WorkflowHumanTask
	if err := a.DB.WithContext(ctx).Where("status='pending' AND expires_at<=?", now).Order("workflow_id,run_id,id").Limit(100).Find(&tasks).Error; err != nil {
		return err
	}
	for _, task := range tasks {
		if err := a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockHumanTaskWorkflow(tx, task.WorkflowID); err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&db.WorkflowRun{}, task.RunID).Error; err != nil {
				return err
			}
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&task, task.ID).Error; err != nil {
				return err
			}
			if task.Status != "pending" || task.ExpiresAt.After(now) {
				return nil
			}
			if err := finishWorkflowHumanTask(tx, &task, "expired", nil, nil, now); err != nil {
				return err
			}
			return resumeWorkflowHumanTaskRun(tx, task.RunID, now)
		}); err != nil {
			return err
		}
	}
	return nil
}
func lockHumanTaskWorkflow(tx *gorm.DB, workflowID int64) error {
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?,0))", fmt.Sprintf("human-tasks:%d", workflowID)).Error
}
func setHumanTaskWaiting(tx *gorm.DB, run db.WorkflowRun, nodeID string, now time.Time) error {
	r := runLeaseQuery(tx, run, false).Updates(map[string]any{"status": RunStatusWaiting, "lease_token": nil, "lease_expires_at": nil, "updated_at": now})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrConflict
	}
	r = tx.Model(&db.WorkflowRunNode{}).Where("run_id=? AND node_instance_id=? AND status=?", run.ID, nodeID, RunStatusRunning).Updates(map[string]any{"status": RunStatusWaiting, "completed_at": now, "duration_ms": gorm.Expr("GREATEST(0, EXTRACT(EPOCH FROM (? - started_at))*1000)::bigint", now)})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected != 1 {
		return ErrConflict
	}
	return nil
}

func finishWorkflowHumanTask(tx *gorm.DB, task *db.WorkflowHumanTask, status string, decision []byte, actorID *int64, now time.Time) error {
	if len(decision) == 0 {
		decision = []byte(`{}`)
	}
	result := tx.Model(task).Where("status = 'pending'").Updates(map[string]any{
		"status": status, "decision_json": string(decision), "decided_by": actorID,
		"decided_at": now, "updated_at": now,
	})
	if result.Error != nil {
		return errors.New("保存人工待办结果失败")
	}
	if result.RowsAffected != 1 {
		return ErrConflict
	}
	task.Status, task.DecisionJSON, task.DecidedBy, task.DecidedAt, task.UpdatedAt = status, string(decision), actorID, &now, now
	return nil
}

func closeWorkflowRunResources(tx *gorm.DB, runID int64, now time.Time) error {
	if err := tx.Model(&db.WorkflowHumanTask{}).Where("run_id=? AND status='pending'", runID).Updates(map[string]any{"status": "superseded", "decided_at": now, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Exec("UPDATE plugin_references SET active=FALSE WHERE reference_type='run' AND reference_id=?", fmt.Sprint(runID)).Error
}

func resumeWorkflowHumanTaskRun(tx *gorm.DB, runID int64, now time.Time) error {
	if err := tx.Model(&db.WorkflowRun{}).Where("id = ? AND status = ?", runID, RunStatusWaiting).Updates(map[string]any{
		"status": RunStatusQueued, "not_before": now, "lease_token": nil, "lease_expires_at": nil, "updated_at": now,
	}).Error; err != nil {
		return errors.New("恢复人工待办对应的运行失败")
	}
	return nil
}

func workflowHumanTaskView(task db.WorkflowHumanTask) WorkflowHumanTaskView {
	view := WorkflowHumanTaskView{
		ID: task.ID, WorkflowID: task.WorkflowID, RunID: task.RunID, NodeInstanceID: task.NodeInstanceID,
		TaskType: task.TaskType, BusinessKey: task.BusinessKey, Prompt: task.Prompt, Status: task.Status,
		ExpiresAt: formatWorkflowTime(task.ExpiresAt), CreatedAt: formatWorkflowTime(task.CreatedAt),
	}
	if task.DecidedAt != nil {
		view.DecidedAt = formatWorkflowTime(*task.DecidedAt)
	}
	return view
}
