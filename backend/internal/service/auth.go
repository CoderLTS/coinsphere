package service

import (
	"context"
	"strings"
	"time"

	"coinsphere/backend/internal/db"
	"coinsphere/backend/internal/security"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Principal is the sole authenticated account and the exact session used by a request.
type Principal struct {
	User           *db.SystemUser
	AccessTokenID  string
	AccessTokenExp time.Time
}

func (p *Principal) IsOwner() bool {
	return p != nil && p.User != nil && p.User.ID > 0 && p.User.Username == "coinsphere" && p.User.IsActive
}

// AuthSession 是登录后签发的短期 access-token 会话。
type AuthSession struct {
	UserID      int64
	AccessToken string
}

// Login 校验凭据并签发 access token。
func (a *App) Login(username, password string, keepLoggedIn bool) (*AuthSession, error) {
	var user db.SystemUser
	lookupErr := a.DB.Where("username = ?", username).First(&user).Error
	hashToCheck := a.dummyHash
	if lookupErr == nil {
		hashToCheck = user.PasswordHash
	}
	passwordOK := a.Hasher.VerifyPassword(password, hashToCheck)
	if lookupErr != nil || user.Username != "coinsphere" || !user.IsActive || !passwordOK {
		return nil, bizErr("用户名或密码错误")
	}
	accessToken := a.Tokens.CreateAccessToken(user.ID, user.SessionVersion, keepLoggedIn)
	now := time.Now().UTC()
	a.DB.Model(&db.SystemUser{}).Where("id = ?", user.ID).
		Updates(map[string]any{"last_login_at": now, "updated_at": now})
	return &AuthSession{
		UserID: user.ID, AccessToken: accessToken.Value,
	}, nil
}

// AuthenticateAccessToken 校验唯一账号、会话版本、过期和撤销状态。
func (a *App) AuthenticateAccessToken(rawToken string) (*Principal, error) {
	payload, err := a.Tokens.VerifyAccessToken(rawToken)
	if err != nil {
		return nil, err
	}
	if a.isAccessTokenRevoked(payload.TokenID) {
		return nil, security.ErrInvalidToken
	}
	principal, err := a.buildPrincipal(payload.UserID)
	if err != nil {
		return nil, err
	}
	if principal.User.SessionVersion != payload.SessionVersion {
		return nil, security.ErrInvalidToken
	}
	principal.AccessTokenID = payload.TokenID
	principal.AccessTokenExp = payload.ExpiresAt
	return principal, nil
}

func (a *App) buildPrincipal(userID int64) (*Principal, error) {
	var user db.SystemUser
	if err := a.DB.Where("id=? AND username='coinsphere' AND is_active", userID).First(&user).Error; err != nil {
		return nil, security.ErrInvalidToken
	}
	return &Principal{User: &user}, nil
}

func (a *App) BuildUserInfo(p *Principal) M {
	return M{"userId": p.User.ID, "username": p.User.Username, "email": p.User.Email, "avatar": p.User.Avatar, "accessMode": "authenticated"}
}

// Password changes and commands share a transaction lock so an invalidated session cannot commit later.
func (a *App) ChangePassword(ctx context.Context, p *Principal, currentPassword, newPassword string) error {
	if len([]rune(newPassword)) < 6 || len(newPassword) > 1024 || len(currentPassword) > 1024 {
		return bizErr("新密码需为 6 至 1024 个字符")
	}
	return a.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := commandPrincipalTx(tx, WithPrincipal(ctx, p))
		if err != nil {
			return err
		}
		if !a.Hasher.VerifyPassword(currentPassword, current.User.PasswordHash) {
			return security.ErrInvalidToken
		}
		return tx.Model(&db.SystemUser{}).Where("id=?", current.User.ID).Updates(map[string]any{
			"password_hash": a.Hasher.HashPassword(newPassword), "session_version": gorm.Expr("session_version + 1"), "updated_at": time.Now().UTC(),
		}).Error
	})
}

type reauthTokenRecord struct {
	userID        int64
	accessTokenID string
	expiresAt     time.Time
}

const reauthTokenTTL = 5 * time.Minute

// Reauthenticate verifies the current password before issuing a one-time token
// bound to the exact access-token session.
func (a *App) Reauthenticate(principal *Principal, password string) (string, error) {
	if !principal.IsOwner() ||
		principal.AccessTokenID == "" || strings.TrimSpace(password) == "" {
		return "", security.ErrInvalidToken
	}

	if _, err := a.RevalidateSession(principal); err != nil {
		return "", err
	}
	var user db.SystemUser
	if err := a.DB.First(&user, principal.User.ID).Error; err != nil || user.SessionVersion != principal.User.SessionVersion || !user.IsActive ||
		!a.Hasher.VerifyPassword(password, user.PasswordHash) {
		return "", security.ErrInvalidToken
	}
	return a.issueReauthToken(principal, time.Now()), nil
}

func (a *App) issueReauthToken(principal *Principal, now time.Time) string {
	raw := security.RandomURLSafe(32)
	hash := security.HashToken(raw)
	a.authStateMu.Lock()
	a.pruneAuthStateLocked(now)
	a.reauthTokens[hash] = reauthTokenRecord{
		userID: principal.User.ID, accessTokenID: principal.AccessTokenID,
		expiresAt: now.Add(reauthTokenTTL),
	}
	a.authStateMu.Unlock()
	return raw
}

// ConsumeReauthToken permits exactly one matching user/session use. A token
// presented by another user or session remains unusable and cannot be consumed.
func (a *App) ConsumeReauthToken(raw string, principal *Principal) bool {
	if !principal.IsOwner() || principal.AccessTokenID == "" || raw == "" {
		return false
	}
	if _, err := a.RevalidateSession(principal); err != nil {
		return false
	}
	hash := security.HashToken(raw)
	now := time.Now().UTC()
	a.authStateMu.Lock()
	defer a.authStateMu.Unlock()
	a.pruneAuthStateLocked(now)
	record, ok := a.reauthTokens[hash]
	if !ok || record.userID != principal.User.ID || record.accessTokenID != principal.AccessTokenID {
		return false
	}
	delete(a.reauthTokens, hash)
	return true
}

func (a *App) LogoutAccessToken(principal *Principal) error {
	if principal == nil || principal.AccessTokenID == "" {
		return security.ErrInvalidToken
	}
	return a.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockAuthorization(tx); err != nil {
			return err
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&db.RevokedSession{TokenID: principal.AccessTokenID, ExpiresAt: principal.AccessTokenExp}).Error
	})
}
func (a *App) isAccessTokenRevoked(tokenID string) bool {
	if tokenID == "" {
		return true
	}
	var count int64
	if err := a.DB.Model(&db.RevokedSession{}).Where("token_id=? AND expires_at>CURRENT_TIMESTAMP", tokenID).Count(&count).Error; err != nil {
		return true
	}
	return count > 0
}

func (a *App) pruneAuthStateLocked(now time.Time) {
	for hash, record := range a.reauthTokens {
		if !record.expiresAt.After(now) {
			delete(a.reauthTokens, hash)
		}
	}

}
