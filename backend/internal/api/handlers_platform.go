package api

import (
	"coinsphere/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func (s *Server) handlePublishWorkflowRevision(c *gin.Context) {
	id, err := pathInt64(c, "workflowId")
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	payload, err := decodeBody[service.WorkflowPublishPayload](c)
	if err != nil {
		respond(c, nil, err, "")
		return
	}
	data, err := s.App.PublishWorkflowRevision(c.Request.Context(), id, *payload, currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handlePluginCatalog(c *gin.Context) {
	ok(c, M{"items": s.App.PluginCatalog(c.Request.Context())})
}
