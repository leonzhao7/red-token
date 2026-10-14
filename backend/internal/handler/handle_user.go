package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"red-token/internal/domain"
	"red-token/internal/store"
)

type userCtxKey struct{}

// SessionCookieName holds the opaque session token issued at login.
const SessionCookieName = "rt_session"

type UserHandler struct {
	store *store.Store
}

func NewUserHandler(st *store.Store) *UserHandler {
	return &UserHandler{store: st}
}

type userView struct {
	domain.User
	IsDefaultPassword bool `json:"is_default_password"`
}

func newUserView(user domain.User) userView {
	return userView{
		User:              user,
		IsDefaultPassword: store.VerifyPassword(user.PasswordHash, store.DefaultPassword),
	}
}

// SessionUserFromContext returns the account authenticated for this request.
func SessionUserFromContext(ctx context.Context) (domain.User, bool) {
	user, ok := ctx.Value(userCtxKey{}).(domain.User)
	return user, ok
}

// RequireSession rejects requests without a valid session cookie. It guards
// every admin API route except login and logout.
func (a *UserHandler) RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		user, err := a.store.FindUserBySession(r.Context(), cookie.Value)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if user == nil {
			clearSessionCookie(w)
			writeError(w, http.StatusUnauthorized, "session expired")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userCtxKey{}, *user)))
	})
}

func (a *UserHandler) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	username := strings.TrimSpace(payload.Username)
	user, err := a.store.FindUserByUsername(r.Context(), username)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Same message for unknown user, wrong password, and disabled account so
	// the response does not reveal which accounts exist.
	if user == nil || !user.Enabled || !store.VerifyPassword(user.PasswordHash, payload.Password) {
		_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
			Level:        "warn",
			Type:         "admin_login_failed",
			Actor:        username,
			ResourceType: "user",
			Message:      "login failed: " + username,
		})
		writeError(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}

	token, expiresAt, err := a.store.CreateSession(r.Context(), user.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.TouchUserLogin(r.Context(), user.ID)
	setSessionCookie(w, token, expiresAt)
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Type:         "admin_login",
		Actor:        user.Username,
		ResourceType: "user",
		ResourceID:   user.ID,
		Message:      "login succeeded: " + user.Username,
	})

	refreshed, err := a.store.GetUser(r.Context(), user.ID)
	if err != nil {
		refreshed = *user
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": newUserView(refreshed)})
}

func (a *UserHandler) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		_ = a.store.DeleteSession(r.Context(), cookie.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *UserHandler) HandleCurrentUser(w http.ResponseWriter, r *http.Request) {
	user, ok := SessionUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": newUserView(user)})
}

func (a *UserHandler) HandleUpdateUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, ok := SessionUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	target, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	var payload struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if message, ok := validatePassword(payload.NewPassword); !ok {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	// Changing your own password requires the current one so an unattended
	// session cannot be used to lock the owner out.
	if id == current.ID && !store.VerifyPassword(target.PasswordHash, payload.CurrentPassword) {
		writeError(w, http.StatusBadRequest, "当前密码不正确")
		return
	}

	hash, err := store.HashPassword(payload.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := a.store.SetUserPassword(r.Context(), id, hash); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Invalidate everything issued under the old password, then re-issue a
	// cookie for the caller so a self-service change does not log them out.
	if err := a.store.DeleteUserSessions(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if id == current.ID {
		token, expiresAt, err := a.store.CreateSession(r.Context(), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		setSessionCookie(w, token, expiresAt)
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_user_password_update",
		Actor:        actorName(r.Context()),
		ResourceType: "user",
		ResourceID:   id,
		Message:      "user password updated: " + target.Username,
	})

	updated, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": newUserView(updated)})
}

func (a *UserHandler) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, ok := SessionUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	// Keeps at least one reachable account: you cannot remove yourself.
	if id == current.ID {
		writeError(w, http.StatusBadRequest, "不能删除当前登录的账号")
		return
	}
	user, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}
	if user.Username == store.DefaultUsername {
		writeError(w, http.StatusBadRequest, "默认管理员账号不能删除")
		return
	}
	if err := a.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_user_delete",
		Actor:        actorName(r.Context()),
		ResourceType: "user",
		ResourceID:   user.ID,
		Message:      "user deleted: " + user.Username,
	})
	writeJSON(w, http.StatusOK, map[string]any{"deleted": id})
}

func setSessionCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		MaxAge:   int(time.Until(expiresAt).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func actorName(ctx context.Context) string {
	if user, ok := SessionUserFromContext(ctx); ok {
		return user.Username
	}
	return "admin"
}

func validateUsername(username string) (string, bool) {
	if username == "" {
		return "用户名不能为空", false
	}
	if len(username) > 64 {
		return "用户名不能超过 64 个字符", false
	}
	if strings.ContainsAny(username, " \t\r\n") {
		return "用户名不能包含空格", false
	}
	return "", true
}

func validatePassword(password string) (string, bool) {
	if len(password) < 4 {
		return "密码至少需要 4 个字符", false
	}
	if len(password) > 128 {
		return "密码不能超过 128 个字符", false
	}
	return "", true
}

func (a *UserHandler) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := a.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	views := make([]userView, 0, len(users))
	for _, user := range users {
		views = append(views, newUserView(user))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": views, "total": len(views)})
}

func (a *UserHandler) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Enabled  bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	username := strings.TrimSpace(payload.Username)
	if message, ok := validateUsername(username); !ok {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	if message, ok := validatePassword(payload.Password); !ok {
		writeError(w, http.StatusBadRequest, message)
		return
	}

	hash, err := store.HashPassword(payload.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	user, err := a.store.CreateUser(r.Context(), username, hash, payload.Enabled)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "用户名已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_user_create",
		Actor:        actorName(r.Context()),
		ResourceType: "user",
		ResourceID:   user.ID,
		Message:      "user created: " + user.Username,
	})
	writeJSON(w, http.StatusCreated, map[string]any{"user": newUserView(user)})
}

func (a *UserHandler) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	current, ok := SessionUserFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	existing, err := a.store.GetUser(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "用户不存在")
		return
	}

	var payload struct {
		Username string `json:"username"`
		Enabled  bool   `json:"enabled"`
	}
	if err := decodeJSON(r, &payload); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	username := strings.TrimSpace(payload.Username)
	if message, ok := validateUsername(username); !ok {
		writeError(w, http.StatusBadRequest, message)
		return
	}
	// Disabling your own account would lock you out on the next request.
	if id == current.ID && !payload.Enabled {
		writeError(w, http.StatusBadRequest, "不能停用当前登录的账号")
		return
	}

	user, err := a.store.UpdateUser(r.Context(), id, username, payload.Enabled)
	if err != nil {
		if errors.Is(err, store.ErrUsernameTaken) {
			writeError(w, http.StatusConflict, "用户名已存在")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A disabled account must not keep browsing with an existing cookie.
	if existing.Enabled && !user.Enabled {
		_ = a.store.DeleteUserSessions(r.Context(), user.ID)
	}
	_ = a.store.AppendAuditEvent(r.Context(), domain.AuditEvent{
		Level:        "warn",
		Type:         "admin_user_update",
		Actor:        actorName(r.Context()),
		ResourceType: "user",
		ResourceID:   user.ID,
		Message:      "user updated: " + user.Username,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": newUserView(user)})
}
