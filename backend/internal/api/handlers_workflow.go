package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"coinsphere/backend/internal/service"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gin-gonic/gin"
)

const maxWorkflowRequestBytes = 2 << 20
const maxWorkflowEventRequestBytes = 1 << 20
const maxWorkflowHumanDecisionBytes = 64 << 10

func (s *Server) handlePublishWorkflowWebhook(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowEventRequestBytes)
	payload, err := decodeBody[json.RawMessage](c)
	if err != nil || payload == nil {
		writeProblem(c, http.StatusBadRequest, "Webhook 请求体须为不超过 1 MiB 的 JSON 对象")
		return
	}
	data, err := s.App.PublishWorkflowWebhook(c.Request.Context(), workflowID, c.Request, *payload)
	respond(c, data, err, "")
}

func (s *Server) handlePublishWorkflowEvent(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	raw, err := decodeBody[json.RawMessage](c)
	if err != nil || len(*raw) > 1<<20 {
		writeProblem(c, http.StatusBadRequest, "CloudEvent 须为不超过 1 MiB 的 JSON 对象")
		return
	}
	var event cloudevents.Event
	if json.Unmarshal(*raw, &event) != nil {
		writeProblem(c, http.StatusBadRequest, "CloudEvent 无效")
		return
	}
	data, err := s.App.PublishWorkflowEvent(c.Request.Context(), event)
	respond(c, data, err, "")
}

func (s *Server) handleListWorkflowHumanTasks(c *gin.Context) {
	page, ok := cursorPage(c)
	if !ok {
		return
	}
	workflowID, err := optionalWorkflowQueryID(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	runID, err := optionalWorkflowQueryID(c, "runId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.PageWorkflowHumanTasks(c.Request.Context(), service.WorkflowHumanTaskQuery{Page: page, Status: queryStr(c, "status"), WorkflowID: workflowID, RunID: runID})
	respond(c, data, err, "")
}

func optionalWorkflowQueryID(c *gin.Context, key string) (int64, error) {
	values, present := c.GetQueryArray(key)
	if !present {
		return 0, nil
	}
	if len(values) != 1 {
		return 0, fmt.Errorf("%s 必须是单个正整数", key)
	}
	raw := values[0]
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s 必须是正整数", key)
	}
	return value, nil
}

func (s *Server) handleDecideWorkflowHumanTask(c *gin.Context) {
	taskID, err := pathInt64(c, "taskId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowHumanDecisionBytes)
	payload, err := decodeBody[service.WorkflowHumanTaskDecision](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.DecideWorkflowHumanTask(c.Request.Context(), taskID, *payload, currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handleListWorkflowTemplates(c *gin.Context) {
	ok(c, M{"items": s.App.ListWorkflowTemplates()})
}

func (s *Server) handleListWorkflowNodeDefinitions(c *gin.Context) {
	ok(c, M{"items": s.App.ListWorkflowNodeDefinitions()})
}

func (s *Server) handleListWorkflowGroups(c *gin.Context) {
	items, err := s.App.ListWorkflowGroups(c.Request.Context())
	respond(c, M{"items": items}, err, "")
}

func (s *Server) handleCreateWorkflowGroup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowGroupUpsertPayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.CreateWorkflowGroup(c.Request.Context(), *payload)
	respond(c, data, err, "")
}

func (s *Server) handleUpdateWorkflowGroup(c *gin.Context) {
	groupID, err := pathInt64(c, "groupId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowGroupUpsertPayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.UpdateWorkflowGroup(c.Request.Context(), groupID, *payload)
	respond(c, data, err, "")
}

func (s *Server) handleDeleteWorkflowGroup(c *gin.Context) {
	groupID, err := pathInt64(c, "groupId")
	if err == nil {
		err = s.App.DeleteWorkflowGroup(c.Request.Context(), groupID)
	}
	respond(c, M{"id": groupID}, err, "")
}

func (s *Server) handleUpdateWorkflowGroupOrder(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowGroupOrderPayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.App.UpdateWorkflowGroupOrder(c.Request.Context(), *payload)
	respond(c, M{"items": items}, err, "")
}

func (s *Server) handleAssignWorkflowGroup(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowGroupAssignmentPayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.AssignWorkflowGroup(c.Request.Context(), *payload)
	respond(c, data, err, "")
}

func (s *Server) handleValidateWorkflowGraph(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[struct {
		Graph json.RawMessage `json:"graph"`
	}](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.App.ValidateWorkflowGraph(payload.Graph); err != nil {
		var issue *service.WorkflowValidationIssue
		if !errors.As(err, &issue) {
			issue = &service.WorkflowValidationIssue{Scope: "graph", Level: "error", Code: "graph.invalid", Message: err.Error()}
		}
		ok(c, M{"valid": false, "issues": []*service.WorkflowValidationIssue{issue}})
		return
	}
	ok(c, M{"valid": true, "issues": []M{}})
}

func (s *Server) handleListWorkflows(c *gin.Context) {
	page, valid := cursorPage(c)
	if !valid {
		return
	}
	data, err := s.App.PageWorkflows(c.Request.Context(), page, queryStr(c, "status"), queryStr(c, "keyword"), queryInt64Ptr(c, "groupId"))
	respond(c, data, err, "")
}

func (s *Server) handleCreateWorkflow(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowCreatePayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.CreateWorkflow(c.Request.Context(), *payload, currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handleGetWorkflow(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.GetWorkflow(c.Request.Context(), workflowID)
	respond(c, data, err, "")
}

func (s *Server) handleUpdateWorkflow(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowUpdatePayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.UpdateWorkflow(c.Request.Context(), workflowID, *payload)
	respond(c, data, err, "")
}

func (s *Server) handleSaveWorkflowRevision(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowRevisionSavePayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.SaveWorkflowRevision(c.Request.Context(), workflowID, *payload, currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handleListWorkflowRevisions(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	items, err := s.App.ListWorkflowRevisions(c.Request.Context(), workflowID)
	respond(c, M{"items": items}, err, "")
}

func (s *Server) handleGetWorkflowRevision(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	revisionID, err := pathInt64(c, "revisionId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.GetWorkflowRevision(c.Request.Context(), workflowID, revisionID)
	respond(c, data, err, "")
}

func (s *Server) handleDeleteWorkflowRevision(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	revisionID, err := pathInt64(c, "revisionId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	err = s.App.DeleteWorkflowRevision(c.Request.Context(), workflowID, revisionID)
	respond(c, M{"id": revisionID}, err, "")
}

func (s *Server) handleDeleteWorkflow(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err == nil {
		err = s.App.DeleteWorkflow(c.Request.Context(), workflowID)
	}
	respond(c, M{"id": workflowID}, err, "")
}

func (s *Server) handleWorkflowLifecycle(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowLifecyclePayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.ApplyWorkflowLifecycle(c.Request.Context(), workflowID, *payload)
	respond(c, data, err, "")
}

func (s *Server) handleListWorkflowRuns(c *gin.Context) {
	workflowID, err := optionalWorkflowQueryID(c, "workflowId")
	if c.Param("workflowId") != "" {
		workflowID, err = pathInt64(c, "workflowId")
	}
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	page, ok := cursorPage(c)
	if !ok {
		return
	}
	triggerType := queryStr(c, "triggerType")
	if triggerType != "" && triggerType != "manual" && triggerType != "schedule" && triggerType != "event" &&
		triggerType != "stream" && triggerType != "webhook" && triggerType != "failure" {
		writeProblem(c, http.StatusBadRequest, "工作流运行触发方式无效")
		return
	}
	status := queryStr(c, "status")
	statusMap := map[string]string{
		"queued": "queued", "running": "running", "waiting": "waiting", "retrying": "retrying",
		"retry_waiting": "retrying", "success": "succeeded", "succeeded": "succeeded",
		"failed": "failed", "canceled": "cancelled", "cancelled": "cancelled",
	}
	if status != "" {
		mapped, valid := statusMap[status]
		if !valid {
			writeProblem(c, http.StatusBadRequest, "工作流运行状态无效")
			return
		}
		status = mapped
	}
	from, err := workflowRunQueryTime(c, "from")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	to, err := workflowRunQueryTime(c, "to")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	if from != nil && to != nil && from.After(*to) {
		writeProblem(c, http.StatusBadRequest, "开始时间不能晚于结束时间")
		return
	}
	keyword := queryStr(c, "keyword")
	if len(keyword) > 200 {
		writeProblem(c, http.StatusBadRequest, "关键词不能超过 200 字节")
		return
	}
	data, err := s.App.PageWorkflowRuns(c.Request.Context(), workflowID, service.WorkflowRunListQuery{
		Page: page, TriggerType: triggerType, Status: status, From: from, To: to, Keyword: keyword,
	})
	respond(c, data, err, "")
}

func (s *Server) handleCreateWorkflowRun(c *gin.Context) {
	workflowID, err := pathInt64(c, "workflowId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload := service.WorkflowRunCreatePayload{}
	if c.Request.ContentLength != 0 {
		decoded, decodeErr := decodeBody[service.WorkflowRunCreatePayload](c)
		if decodeErr != nil {
			writeProblem(c, http.StatusBadRequest, decodeErr.Error())
			return
		}
		payload = *decoded
	}
	data, err := s.App.CreateWorkflowRun(c.Request.Context(), workflowID, payload, currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handleGetWorkflowRun(c *gin.Context) {
	runID, err := pathInt64(c, "runId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.GetWorkflowRunDetail(c.Request.Context(), runID)
	respond(c, data, err, "")
}

func (s *Server) handleGetWorkflowArtifactManifest(c *gin.Context) {
	data, err := s.App.GetWorkflowArtifactManifest(c.Request.Context(), c.Param("sha256"), true)
	respond(c, data, err, "")
}

func workflowRunQueryTime(c *gin.Context, name string) (*time.Time, error) {
	raw := queryStr(c, name)
	if raw == "" {
		return nil, nil
	}
	value, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("%s 须为 RFC3339 格式的 UTC 时间", name)
	}
	_, offset := value.Zone()
	if offset != 0 {
		return nil, fmt.Errorf("%s 须使用 UTC 时间", name)
	}
	value = value.UTC()
	return &value, nil
}

func (s *Server) handleDownloadWorkflowArtifact(c *gin.Context) {
	reader, artifact, err := s.App.OpenWorkflowArtifact(c.Request.Context(), c.Param("sha256"))
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	defer reader.Close()
	c.Header("Content-Type", artifact.MediaType)
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", artifact.SHA256))
	c.Header("Content-Length", strconv.FormatInt(artifact.SizeBytes, 10))
	c.Header("ETag", fmt.Sprintf("\"%s\"", artifact.SHA256))
	c.Header("X-Content-SHA256", artifact.SHA256)
	c.Status(http.StatusOK)
	_, _ = io.Copy(c.Writer, reader)
}

func (s *Server) handleWorkflowRunAction(c *gin.Context) {
	runID, err := pathInt64(c, "runId")
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxWorkflowRequestBytes)
	payload, err := decodeBody[service.WorkflowRunActionPayload](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, err.Error())
		return
	}
	data, err := s.App.ApplyWorkflowRunAction(c.Request.Context(), runID, *payload)
	respond(c, data, err, "")
}
