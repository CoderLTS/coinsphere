package service

import (
	"coinsphere/backend/internal/db"
	"coinsphere/backend/plugin/sdk"
	"context"
	"encoding/json"
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"strings"
	"time"
)

func (a *App) DeliverInbox(ctx context.Context, request sdk.InboxRequest) (json.RawMessage, error) {
	if len(request.Title) > 255 || len(request.Message) > 10000 || request.OperationKey == "" || len(request.SubjectKey) > 128 {
		return nil, errors.New("站内通知请求无效")
	}
	workflowID, revisionID, err := request.Revision.IDs()
	if err != nil {
		return nil, err
	}
	recipients, err := a.inboxRecipients(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	err = a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lease, ok := ctx.Value(executionLeaseKey{}).(db.WorkflowRun)
		if !ok || lease.WorkflowID != workflowID || lease.RevisionID != revisionID {
			return ErrPermission
		}
		if err := a.lockExecutionLease(tx, lease, nil); err != nil {
			return err
		}
		var runNode db.WorkflowRunNode
		if err := tx.Where("run_id=? AND node_instance_id=? AND operation_key=? AND status=?", lease.ID, request.NodeInstanceID, request.OperationKey, RunStatusRunning).First(&runNode).Error; err != nil {
			return ErrPermission
		}
		for _, userID := range recipients {
			delivery := db.NotificationDelivery{OperationKey: request.OperationKey, WorkflowID: workflowID, RevisionID: revisionID, NodeInstanceID: request.NodeInstanceID, Channel: "in_app", RecipientUserID: &userID, SubjectKey: request.SubjectKey, Title: strings.TrimSpace(request.Title), Message: request.Message, Status: "delivered", AttemptCount: 1, DeliveredAt: &now, CreatedAt: now, UpdatedAt: now}
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&delivery).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	var deliveries []db.NotificationDelivery
	if err := a.DB.WithContext(ctx).Where("operation_key=? AND recipient_user_id IN ?", request.OperationKey, recipients).Order("id").Find(&deliveries).Error; err != nil {
		return nil, err
	}
	if len(deliveries) != len(recipients) {
		return nil, errors.New("站内通知收件人不匹配")
	}
	ids := []int64{}
	for _, d := range deliveries {
		ids = append(ids, d.ID)
		a.PublishInAppNotification(ctx, *d.RecipientUserID, d.ID)
	}
	return json.Marshal(M{"deliveryId": ids[0], "deliveryIds": ids, "recipientCount": len(ids), "channel": "in_app", "status": "delivered", "deliveredAt": formatWorkflowTime(*deliveries[0].DeliveredAt)})
}
func (a *App) inboxRecipients(ctx context.Context, workflowID int64) ([]int64, error) {
	var owner db.SystemUser
	if err := a.DB.WithContext(ctx).Where("username='coinsphere' AND is_active").First(&owner).Error; err != nil {
		return nil, errors.New("个人账号通知收件人不可用")
	}
	var count int64
	if err := a.DB.WithContext(ctx).Model(&db.Workflow{}).Where("id=?", workflowID).Count(&count).Error; err != nil || count != 1 {
		return nil, ErrNotFound
	}
	return []int64{owner.ID}, nil
}
