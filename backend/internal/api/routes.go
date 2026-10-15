package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"coinsphere/backend/internal/service"
	"coinsphere/backend/plugin/sdk"
	"github.com/gin-gonic/gin"
)

// registerRoutes 只暴露 个人微内核仍保留的认证、监控与系统管理边界。
func (s *Server) registerRoutes(router *gin.Engine) {
	get(router, "/health", s.handleReady)
	get(router, "/health/live", func(c *gin.Context) {
		writeJSON(c, http.StatusOK, M{"status": "alive"})
	})
	get(router, "/health/ready", s.handleReady)
	get(router, "/metrics", s.requireAuth(), s.handleMetrics)

	_ = os.MkdirAll(s.StaticDir, 0o755)
	_ = os.MkdirAll(filepath.Join(s.UploadsDir, "avatars"), 0o755)
	router.StaticFS("/static", http.Dir(s.StaticDir))
	router.StaticFS("/uploads", http.Dir(s.UploadsDir))

	api := router.Group("/api/v1")
	authenticated := api.Group("", s.requireAuth())
	auth := api.Group("/auth")
	auth.POST("/login", s.rateLimit(), s.handleLogin)
	auth.POST("/logout", s.requireAuth(), s.handleLogout)
	auth.POST("/reauth", s.requireAuth(), s.handleReauth)
	get(authenticated, "/me", s.handleMe)
	authenticated.PUT("/me/password", s.handleChangePassword)

	get(authenticated, "/home/meta", func(c *gin.Context) {
		ok(c, s.App.GetHomeMeta())
	})
	get(authenticated, "/home/overview", func(c *gin.Context) {
		data, err := s.App.GetHomeOverview(c.Request.Context())
		if err == nil {
			for key, value := range s.metrics.snapshot() {
				data[key] = value
			}
		}
		respond(c, data, err, "")
	})

	api.POST("/webhooks/:workflowId", s.handlePublishWorkflowWebhook)
	get(authenticated, "/workflows/templates", s.handleListWorkflowTemplates)
	authenticated.POST("/events", s.handlePublishWorkflowEvent)
	get(authenticated, "/human-tasks", s.handleListWorkflowHumanTasks)
	authenticated.POST("/human-tasks/:taskId", s.handleDecideWorkflowHumanTask)
	get(authenticated, "/workflows/node-definitions", s.handleListWorkflowNodeDefinitions)
	authenticated.POST("/workflows/validate", s.handleValidateWorkflowGraph)
	get(authenticated, "/workflow-groups", s.handleListWorkflowGroups)
	authenticated.POST("/workflow-groups", s.handleCreateWorkflowGroup)
	authenticated.PUT("/workflow-groups/order", s.handleUpdateWorkflowGroupOrder)
	authenticated.PATCH("/workflow-groups/:groupId", s.handleUpdateWorkflowGroup)
	authenticated.DELETE("/workflow-groups/:groupId", s.handleDeleteWorkflowGroup)
	get(authenticated, "/workflows", s.handleListWorkflows)
	authenticated.POST("/workflows", s.handleCreateWorkflow)
	authenticated.PATCH("/workflows/group-assignment", s.handleAssignWorkflowGroup)
	get(authenticated, "/workflows/:workflowId", s.handleGetWorkflow)
	authenticated.PATCH("/workflows/:workflowId", s.handleUpdateWorkflow)
	authenticated.DELETE("/workflows/:workflowId", s.handleDeleteWorkflow)
	get(authenticated, "/workflows/:workflowId/revisions", s.handleListWorkflowRevisions)
	authenticated.POST("/workflows/:workflowId/revisions", s.handleSaveWorkflowRevision)
	get(authenticated, "/workflows/:workflowId/revisions/:revisionId", s.handleGetWorkflowRevision)
	authenticated.DELETE("/workflows/:workflowId/revisions/:revisionId", s.handleDeleteWorkflowRevision)
	authenticated.POST("/workflows/:workflowId/lifecycle", s.handleWorkflowLifecycle)
	get(authenticated, "/workflows/:workflowId/runs", s.handleListWorkflowRuns)
	authenticated.POST("/workflows/:workflowId/runs", s.handleCreateWorkflowRun)
	get(api, "/ws/workflows/:workflowId/runs", s.handleWorkflowRunsWebSocket)
	get(authenticated, "/notification-deliveries", s.handleListInAppNotifications)
	authenticated.POST("/notification-deliveries/:deliveryId/read", s.handleReadInAppNotification)
	authenticated.POST("/notification-deliveries/read-all", s.handleReadAllInAppNotifications)
	get(api, "/ws/notifications", s.handleNotificationWebSocket)
	get(authenticated, "/workflow-runs/:runId", s.handleGetWorkflowRun)
	authenticated.POST("/workflow-runs/:runId", s.handleWorkflowRunAction)
	get(authenticated, "/artifacts/:sha256/manifest", s.handleGetWorkflowArtifactManifest)
	get(authenticated, "/artifacts/:sha256/download", s.handleDownloadWorkflowArtifact)
	s.registerSystemPluginRoutes(authenticated, api)
	s.registerWorkflowPluginRoutes(authenticated)
	authenticated.POST("/workflows/:workflowId/publish", s.handlePublishWorkflowRevision)
	get(authenticated, "/plugins/catalog", s.handlePluginCatalog)
	s.registerResultPluginRoutes(authenticated)

	authenticated.POST("/system/uploads/avatars", s.handleUploadAvatar)
	get(authenticated, "/system/menus", s.handleGetMenus)
	get(authenticated, "/system/menus/manage-tree", s.handleGetManageMenus)
	get(authenticated, "/system/menus/pages", func(c *gin.Context) { items, err := s.App.MenuPageCatalog(); respond(c, items, err, "") })
	get(authenticated, "/system/i18n-dictionaries", s.handleGetI18nDict)
	authenticated.POST("/system/menus", s.handleCreateMenu)
	authenticated.PUT("/system/menus/:menuId", s.handleUpdateMenu)
	authenticated.DELETE("/system/menus/:menuId", s.handleDeleteMenu)
	get(authenticated, "/system/plugins", func(c *gin.Context) {
		items, err := s.App.ListInstalledPlugins()
		respond(c, items, err, "")
	})
	get(authenticated, "/system/proxies", s.handleListOutboundProxies)
	authenticated.POST("/system/proxies", s.handleCreateOutboundProxy)
	authenticated.PUT("/system/proxies/:proxyId", s.handleUpdateOutboundProxy)
	authenticated.PATCH("/system/proxies/:proxyId", s.handlePatchOutboundProxy)
	authenticated.DELETE("/system/proxies/:proxyId", s.handleDeleteOutboundProxy)
	authenticated.POST("/system/proxies/:proxyId/validations", s.handleValidateOutboundProxy)
	get(authenticated, "/system/logs", s.handleListSystemLogs)
	get(authenticated, "/system/logs/runtime", s.handleGetSystemLogRuntime)
	authenticated.PUT("/system/logs/runtime", s.handleUpdateSystemLogRuntime)
}

func get(routes gin.IRoutes, path string, handlers ...gin.HandlerFunc) {
	routes.GET(path, handlers...)
	routes.HEAD(path, handlers...)
}

func registerPluginRoute(routes gin.IRoutes, method, path string, handlers ...gin.HandlerFunc) {
	routes.Handle(method, path, handlers...)
	if method == http.MethodGet {
		routes.Handle(http.MethodHead, path, handlers...)
	}
}

func (s *Server) registerResultPluginRoutes(routes gin.IRoutes) {
	if s.App == nil || s.App.Plugins == nil {
		return
	}
	for _, route := range s.App.Plugins.Routes() {
		if route.Descriptor.Scope != sdk.ScopeResult {
			continue
		}
		registered := route
		directPattern := "/plugins/" + registered.PluginID + "/results" + registered.Descriptor.Pattern
		registerPluginRoute(routes, registered.Descriptor.Method, directPattern, func(c *gin.Context) {
			scope, err := s.App.ResolveDirectResultScope(c.Request.Context(), registered.PluginID, registered.Descriptor.ResultPageKey, registered.Descriptor.Action, c.Request.URL.Query(), currentPrincipal(c))
			if err != nil {
				respond(c, nil, err, "")
				return
			}
			registered.Handler(c, scope)
		})

	}
}

func (s *Server) registerSystemPluginRoutes(routes, publicRoutes gin.IRoutes) {
	if s.App == nil || s.App.Plugins == nil {
		return
	}
	for _, route := range s.App.Plugins.Routes() {
		if route.Descriptor.Scope != sdk.ScopeSystem {
			continue
		}
		registered := route
		pattern := "/plugins/" + route.PluginID + route.Descriptor.Pattern
		if route.Descriptor.WebSocket {
			publicRoutes.GET(pattern, func(c *gin.Context) {
				protocol := "coinsphere.plugin." + registered.PluginID + ".v1"
				token, ok := pluginWebSocketToken(c.Request, protocol)
				principal, err := s.App.AuthenticateAccessToken(token)
				if !ok || err != nil || principal == nil {
					writeProblem(c, http.StatusUnauthorized, "invalid websocket authentication")
					return
				}
				c.Request = c.Request.WithContext(service.WithPrincipal(c.Request.Context(), principal))
				scope, err := s.App.ResolveSystemScope(c.Request.Context(), registered.PluginID)
				if err != nil {
					respond(c, nil, err, "")
					return
				}
				registered.Handler(c, scope)
			})
			continue
		}
		registerPluginRoute(routes, registered.Descriptor.Method, pattern, func(c *gin.Context) {
			scope, err := s.App.ResolveSystemScope(c.Request.Context(), registered.PluginID)
			if err != nil {
				respond(c, nil, err, "")
				return
			}
			registered.Handler(c, scope)
		})
	}
}
func (s *Server) registerWorkflowPluginRoutes(routes gin.IRoutes) {
	for _, route := range s.App.Plugins.Routes() {
		if route.Descriptor.Scope != sdk.ScopeWorkflow {
			continue
		}
		registered := route
		pattern := "/workflows/:workflowId/nodes/:nodeInstanceId/plugins/" + route.PluginID + route.Descriptor.Pattern
		registerPluginRoute(routes, route.Descriptor.Method, pattern, func(c *gin.Context) {
			id, err := pathInt64(c, "workflowId")
			if err != nil {
				respond(c, nil, err, "")
				return
			}
			scope, err := s.App.ResolveWorkflowScope(c.Request.Context(), registered.PluginID, id, c.Param("nodeInstanceId"))
			if err != nil {
				respond(c, nil, err, "")
				return
			}
			registered.Handler(c, scope)
		})
	}
}

func pluginWebSocketToken(request *http.Request, expectedProtocol string) (string, bool) {
	values := request.Header.Values("Sec-WebSocket-Protocol")
	if len(values) != 1 {
		return "", false
	}
	protocols := strings.Split(values[0], ",")
	if len(protocols) != 2 || strings.TrimSpace(protocols[0]) != expectedProtocol {
		return "", false
	}
	token := strings.TrimSpace(protocols[1])
	return token, token != ""
}

func (s *Server) handleReady(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), time.Second)
	defer cancel()
	if err := s.App.DatabaseReady(ctx); err != nil {
		writeJSON(c, http.StatusServiceUnavailable, M{"status": "unavailable"})
		return
	}
	writeJSON(c, http.StatusOK, M{"status": "ready"})
}
