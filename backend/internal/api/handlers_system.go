package api

import (
	"coinsphere/backend/internal/security"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"coinsphere/backend/internal/service"
	"github.com/gin-gonic/gin"
)

func (s *Server) handleLogin(c *gin.Context) {
	payload, err := decodeBody[struct {
		Username     string `json:"username"`
		Password     string `json:"password"`
		KeepLoggedIn bool   `json:"keepLoggedIn"`
	}](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, "invalid login request")
		return
	}
	if payload.Username == "" || payload.Password == "" {
		writeProblem(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	session, err := s.App.Login(payload.Username, payload.Password, payload.KeepLoggedIn)
	if err != nil {
		writeProblem(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	setAuditActor(c, session.UserID)
	ok(c, M{"accessToken": session.AccessToken})
}

func (s *Server) handleLogout(c *gin.Context) {
	respond(c, M{}, s.App.LogoutAccessToken(currentPrincipal(c)), "")
}

func (s *Server) handleReauth(c *gin.Context) {
	payload, err := decodeBody[struct {
		Password string `json:"password"`
	}](c)
	if err != nil || strings.TrimSpace(payload.Password) == "" {
		writeProblem(c, http.StatusBadRequest, "password is required")
		return
	}
	token, err := s.App.Reauthenticate(currentPrincipal(c), payload.Password)
	if err != nil {
		writeProblem(c, http.StatusUnauthorized, "invalid credentials")
		return
	}
	ok(c, M{"reauthToken": token})
}

func (s *Server) handleMe(c *gin.Context) {
	ok(c, s.App.BuildUserInfo(currentPrincipal(c)))
}

var allowedAvatarTypes = map[string]string{
	"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp", "image/gif": ".gif",
}

func (s *Server) handleUploadAvatar(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(4 << 20); err != nil {
		fail(c, "上传内容解析失败")
		return
	}
	file, header, err := c.Request.FormFile("avatar")
	if err != nil {
		fail(c, "缺少 avatar 文件")
		return
	}
	defer file.Close()

	suffix := strings.ToLower(filepath.Ext(header.Filename))
	if normalized, okType := allowedAvatarTypes[header.Header.Get("Content-Type")]; okType {
		suffix = normalized
	}
	if suffix == ".jpeg" {
		suffix = ".jpg"
	}
	validSuffix := map[string]bool{".jpg": true, ".png": true, ".webp": true, ".gif": true}
	if !validSuffix[suffix] {
		fail(c, "Only jpg/png/webp/gif avatars are supported")
		return
	}
	fileBytes, err := io.ReadAll(io.LimitReader(file, 2*1024*1024+1))
	if err != nil || len(fileBytes) == 0 {
		fail(c, "Uploaded file cannot be empty")
		return
	}
	if len(fileBytes) > 2*1024*1024 {
		fail(c, "Avatar image must be 2MB or smaller")
		return
	}
	randomBytes := make([]byte, 16)
	_, _ = rand.Read(randomBytes)
	fileName := hex.EncodeToString(randomBytes) + suffix
	avatarDir := filepath.Join(s.UploadsDir, "avatars")
	_ = os.MkdirAll(avatarDir, 0o755)
	if err := os.WriteFile(filepath.Join(avatarDir, fileName), fileBytes, 0o644); err != nil {
		fail(c, "保存头像失败: "+err.Error())
		return
	}
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	baseURL := scheme + "://" + c.Request.Host
	okMsg(c, M{"url": baseURL + "/uploads/avatars/" + fileName}, "头像上传成功")
}

func (s *Server) handleGetMenus(c *gin.Context) {
	data, err := s.App.GetMenuTree(currentPrincipal(c))
	respond(c, data, err, "")
}

func (s *Server) handleGetManageMenus(c *gin.Context) {
	data, err := s.App.GetMenuManagementTree()
	respond(c, data, err, "")
}

func (s *Server) handleGetI18nDict(c *gin.Context) {
	scope := queryStr(c, "scope")
	if scope == "" {
		scope = "menu"
	}
	ok(c, s.App.GetI18nDict(scope))
}

func (s *Server) handleCreateMenu(c *gin.Context) {
	payload, err := decodeBody[service.MenuUpsertPayload](c)
	if err != nil {
		fail(c, err.Error())
		return
	}
	data, err := s.App.CreateMenu(c.Request.Context(), *payload)
	respond(c, data, err, "菜单创建成功")
}

func (s *Server) handleUpdateMenu(c *gin.Context) {
	menuID, err := pathInt64(c, "menuId")
	if err != nil {
		fail(c, err.Error())
		return
	}
	payload, err := decodeBody[service.MenuUpsertPayload](c)
	if err != nil {
		fail(c, err.Error())
		return
	}
	data, err := s.App.UpdateMenu(c.Request.Context(), menuID, *payload)
	respond(c, data, err, "菜单更新成功")
}

func (s *Server) handleDeleteMenu(c *gin.Context) {
	menuID, err := pathInt64(c, "menuId")
	if err != nil {
		fail(c, err.Error())
		return
	}
	respond(c, nil, s.App.DeleteMenu(c.Request.Context(), menuID), "菜单删除成功")
}

func (s *Server) handleChangePassword(c *gin.Context) {
	payload, err := decodeBody[struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}](c)
	if err != nil {
		writeProblem(c, http.StatusBadRequest, "invalid password request")
		return
	}
	err = s.App.ChangePassword(c.Request.Context(), currentPrincipal(c), payload.CurrentPassword, payload.NewPassword)
	if errors.Is(err, security.ErrInvalidToken) {
		writeProblem(c, http.StatusUnauthorized, "invalid current password")
		return
	}
	respond(c, M{}, err, "")
}
