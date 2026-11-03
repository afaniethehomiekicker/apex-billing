package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"strings"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
)

// Profile settings and profile pictures.
//
//	PUT  /api/me/profile               {name, phone}                      everyone, own profile
//	PUT  /api/me/email                 {email, current_password, code}    admins, own sign-in email
//	POST /api/me/avatar                multipart "file"                   everyone, own picture
//	POST /api/me/avatar/remove
//	GET  /api/users/:id/avatar         the picture (any signed-in user)
//	POST /api/users/:id/avatar         multipart "file"                   admins, for any user
//	POST /api/users/:id/avatar/remove                                     admins
//
// Emails: other users' emails can't be changed (an admin edits everything
// else about them). An admin can change their own sign-in email after
// confirming their password (and two-step code when it's on).

const maxAvatarBytes = 2 << 20

// UpdateMyProfile: the signed-in user's own name and phone.
func UpdateMyProfile(c *gin.Context) {
	var in struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var ok bool
	if in.Name, ok = trimTo(in.Name, 150); !ok {
		badRequest(c, "The name is too long")
		return
	}
	if in.Name == "" {
		badRequest(c, "Enter your name")
		return
	}
	if in.Phone, ok = trimTo(in.Phone, 40); !ok {
		badRequest(c, "The phone number is too long")
		return
	}
	u := auth.User(c)
	var ch changeList
	ch.add("Name", u.Name, in.Name)
	ch.add("Phone", u.Phone, in.Phone)
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"user": me(u)})
		return
	}
	if err := database.DB.Model(u).Updates(map[string]interface{}{"name": in.Name, "phone": in.Phone}).Error; err != nil {
		serverError(c, "Could not save your profile", err)
		return
	}
	u.Name, u.Phone = in.Name, in.Phone
	audit(c, "updated", "user", u.ID, u.UserNumber+" own profile: "+ch.String())
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

// ChangeMyEmail: an admin changes their own sign-in email. Needs the
// current password (and a two-step code when it's on); other sessions end.
func ChangeMyEmail(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"current_password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Password) > 200 || len(in.Code) > 20 {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if a, err := mail.ParseAddress(email); err != nil || a.Address != email || len(email) > 200 {
		badRequest(c, "Enter a valid email address")
		return
	}
	if email == u.Email {
		badRequest(c, "That is already your email")
		return
	}
	keys := []string{"email:" + u.Email, "ip:" + c.ClientIP()}
	if !loginLimiter.Begin(keys...) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many wrong attempts. Try again in 15 minutes."})
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Password) {
		auditAs(c, u, "email_change_failed", "Wrong current password")
		badRequest(c, "Your current password is not right")
		return
	}
	if u.TOTPEnabled {
		if ok, _ := checkSecondFactor(u, in.Code); !ok {
			auditAs(c, u, "email_change_failed", "Wrong two-step code")
			badRequest(c, "Enter the current code from your authenticator app")
			return
		}
	}
	loginLimiter.Forgive(keys...)
	var n int64
	database.DB.Model(&models.User{}).Where("lower(email) = ? AND id <> ?", email, u.ID).Count(&n)
	if n > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "There is already a user with this email"})
		return
	}
	old := u.Email
	if err := database.DB.Model(u).Update("email", email).Error; err != nil {
		serverError(c, "Could not change the email", err)
		return
	}
	u.Email = email
	revoked := auth.RevokeSessions(u.ID, auth.Session(c).ID, "email changed")
	audit(c, "email_changed", "user", u.ID, fmt.Sprintf("%s sign-in email changed from %s to %s; %d other session(s) signed out", u.UserNumber, old, email, revoked))
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

// ---- profile pictures ------------------------------------------------------------

func UploadMyAvatar(c *gin.Context) { setAvatar(c, auth.User(c)) }
func RemoveMyAvatar(c *gin.Context) { clearAvatar(c, auth.User(c)) }

func UploadUserAvatar(c *gin.Context) {
	if u := userFromParam(c); u != nil {
		setAvatar(c, u)
	}
}

func RemoveUserAvatar(c *gin.Context) {
	if u := userFromParam(c); u != nil {
		clearAvatar(c, u)
	}
}

func userFromParam(c *gin.Context) *models.User {
	id, ok := idParam(c, "id")
	if !ok {
		return nil
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return nil
	}
	return &u
}

func whose(c *gin.Context, u *models.User) string {
	if u.ID == auth.User(c).ID {
		return u.UserNumber + " own profile picture"
	}
	return u.UserNumber + " " + u.Name + ": profile picture"
}

func removeAvatarFile(rel string) {
	if rel != "" && !strings.Contains(rel, "..") && strings.HasPrefix(rel, "avatars/") {
		os.Remove(filepath.Join(uploadDir(), filepath.FromSlash(rel)))
	}
}

// setAvatar stores a new picture (PNG, JPG or WEBP up to 2 MB, checked by
// its content, not its name) and removes the old file.
func setAvatar(c *gin.Context, u *models.User) {
	fh, err := c.FormFile("file")
	if err != nil {
		badRequest(c, "Choose a picture (PNG, JPG or WEBP, up to 2 MB)")
		return
	}
	rel, ctype, _, msg, err := saveUploadOf(fh, "avatars", maxAvatarBytes, "picture", logoTypes, "The profile picture must be PNG, JPG or WEBP")
	if msg != "" {
		badRequest(c, msg)
		return
	}
	if err != nil {
		serverError(c, "Could not save the picture", err)
		return
	}
	old := u.AvatarPath
	if err := database.DB.Model(u).Updates(map[string]interface{}{"avatar_path": rel, "avatar_type": ctype}).Error; err != nil {
		removeAvatarFile(rel)
		serverError(c, "Could not save the picture", err)
		return
	}
	removeAvatarFile(old)
	u.AvatarPath, u.AvatarType = rel, ctype
	what := "added"
	if old != "" {
		what = "changed"
	}
	audit(c, "updated", "user", u.ID, whose(c, u)+" "+what)
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

func clearAvatar(c *gin.Context, u *models.User) {
	if u.AvatarPath == "" {
		c.JSON(http.StatusOK, gin.H{"user": me(u)})
		return
	}
	old := u.AvatarPath
	if err := database.DB.Model(u).Updates(map[string]interface{}{"avatar_path": "", "avatar_type": ""}).Error; err != nil {
		serverError(c, "Could not remove the picture", err)
		return
	}
	removeAvatarFile(old)
	u.AvatarPath, u.AvatarType = "", ""
	audit(c, "updated", "user", u.ID, whose(c, u)+" removed")
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

// GetUserAvatar sends a user's picture to any signed-in user.
func GetUserAvatar(c *gin.Context) {
	u := userFromParam(c)
	if u == nil {
		return
	}
	if u.AvatarPath == "" || strings.Contains(u.AvatarPath, "..") {
		c.JSON(http.StatusNotFound, gin.H{"error": "No picture"})
		return
	}
	full := filepath.Join(uploadDir(), filepath.FromSlash(u.AvatarPath))
	if _, err := os.Stat(full); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "No picture"})
		return
	}
	ctype := u.AvatarType
	if _, ok := logoTypes[ctype]; !ok {
		ctype = "application/octet-stream"
	}
	c.Header("Content-Type", ctype)
	c.Header("Cache-Control", "private, no-store")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	c.File(full)
}
