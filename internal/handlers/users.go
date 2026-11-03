package handlers

import (
	"fmt"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
)

// Users of APEX Billing (admin only). Never deleted: disabling an account
// ends its sessions and blocks sign-in, and its history stays.
//
//	GET  /api/users
//	POST /api/users                      {name, email, phone, role, password}
//	PUT  /api/users/:id                  {name, phone, role, active}
//	POST /api/users/:id/password         {password}  (the user picks a new one at next sign-in)

type userInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Role     string `json:"role"`
	Active   *bool  `json:"active"`
	Password string `json:"password"`
}

func cleanUser(in *userInput, creating bool) string {
	var ok bool
	if in.Name, ok = trimTo(in.Name, 150); !ok {
		return "The name is too long"
	}
	if in.Name == "" {
		return "Enter the person's name"
	}
	if in.Phone, ok = trimTo(in.Phone, 40); !ok {
		return "The phone number is too long"
	}
	in.Role = strings.TrimSpace(in.Role)
	if _, ok := models.Roles[in.Role]; !ok {
		return "Choose a role"
	}
	if creating {
		in.Email = strings.ToLower(strings.TrimSpace(in.Email))
		if a, err := mail.ParseAddress(in.Email); err != nil || a.Address != in.Email || len(in.Email) > 200 {
			return "Enter a valid email address"
		}
		if msg := auth.PasswordProblem(in.Password, in.Email, in.Name); msg != "" {
			return msg
		}
	}
	return ""
}

// activeAdmins counts enabled admins other than exceptID.
func activeAdmins(exceptID uint) int64 {
	var n int64
	database.DB.Model(&models.User{}).Where("role = ? AND active = ? AND id <> ?", models.RoleAdmin, true, exceptID).Count(&n)
	return n
}

func GetUsers(c *gin.Context) {
	var users []models.User
	if err := database.DB.Order("active DESC, name ASC").Find(&users).Error; err != nil {
		serverError(c, "Could not load users", err)
		return
	}
	type row struct {
		meView
		Sessions int64 `json:"sessions"`
	}
	type cnt struct {
		UserID uint
		N      int64
	}
	var counts []cnt
	database.DB.Model(&models.Session{}).Select("user_id, COUNT(*) AS n").
		Where("revoked_at IS NULL AND expires_at > ? AND last_seen_at > ?", time.Now(), time.Now().Add(-auth.IdleTimeout)).
		Group("user_id").Scan(&counts)
	active := map[uint]int64{}
	for _, x := range counts {
		active[x.UserID] = x.N
	}
	out := make([]row, 0, len(users))
	for i := range users {
		out = append(out, row{me(&users[i]), active[users[i].ID]})
	}
	roles := []gin.H{}
	for _, k := range []string{models.RoleAdmin, models.RoleAccountant, models.RoleRecorder, models.RoleVerifier} {
		roles = append(roles, gin.H{"key": k, "label": models.Roles[k]})
	}
	c.JSON(http.StatusOK, gin.H{"users": out, "roles": roles})
}

func CreateUser(c *gin.Context) {
	var in userInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	if msg := cleanUser(&in, true); msg != "" {
		badRequest(c, msg)
		return
	}
	var n int64
	database.DB.Model(&models.User{}).Where("email = ?", in.Email).Count(&n)
	if n > 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "There is already a user with this email"})
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		serverError(c, "Could not create the user", err)
		return
	}
	now := time.Now()
	u := models.User{
		Name: in.Name, Email: in.Email, Phone: in.Phone, Role: in.Role, Active: true,
		PasswordHash: hash, PasswordChangedAt: &now, MustChangePassword: true,
	}
	if err := database.DB.Create(&u).Error; err != nil {
		serverError(c, "Could not create the user", err)
		return
	}
	audit(c, "created", "user", u.ID, u.UserNumber+" "+u.Name+" ("+models.Roles[u.Role]+")")
	c.JSON(http.StatusCreated, gin.H{"user": me(&u)})
}

func UpdateUser(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in userInput
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	if msg := cleanUser(&in, false); msg != "" {
		badRequest(c, msg)
		return
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	active := u.Active
	if in.Active != nil {
		active = *in.Active
	}
	self := auth.User(c)
	if u.ID == self.ID && (!active || in.Role != models.RoleAdmin) {
		badRequest(c, "You can't disable your own account or remove your own admin role")
		return
	}
	if u.Role == models.RoleAdmin && u.Active && (!active || in.Role != models.RoleAdmin) && activeAdmins(u.ID) == 0 {
		badRequest(c, "This is the only active admin; make someone else admin first")
		return
	}
	var ch changeList
	ch.add("Name", u.Name, in.Name)
	ch.add("Phone", u.Phone, in.Phone)
	ch.add("Role", models.Roles[u.Role], models.Roles[in.Role])
	if u.Active != active {
		ch.add("Status", map[bool]string{true: "Active", false: "Disabled"}[u.Active], map[bool]string{true: "Active", false: "Disabled"}[active])
	}
	if len(ch) == 0 {
		c.JSON(http.StatusOK, gin.H{"user": me(&u)})
		return
	}
	wasActive := u.Active
	u.Name, u.Phone, u.Role, u.Active = in.Name, in.Phone, in.Role, active
	// Only these columns: saving the whole row could put back a password or
	// two-step setting the user changed a moment ago.
	if err := database.DB.Model(&models.User{}).Where("id = ?", u.ID).Updates(map[string]interface{}{
		"name": u.Name, "phone": u.Phone, "role": u.Role, "active": u.Active,
	}).Error; err != nil {
		serverError(c, "Could not save the user", err)
		return
	}
	if wasActive && !active {
		auth.RevokeSessions(u.ID, 0, "account disabled")
	}
	audit(c, "updated", "user", u.ID, u.UserNumber+" "+ch.String())
	c.JSON(http.StatusOK, gin.H{"user": me(&u)})
}

// SetUserPassword: an admin sets a temporary password; the user's sessions
// end and they choose a new password at next sign-in.
func SetUserPassword(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	if msg := auth.PasswordProblem(in.Password, u.Email, u.Name); msg != "" {
		badRequest(c, msg)
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		serverError(c, "Could not set the password", err)
		return
	}
	now := time.Now()
	if err := database.DB.Model(&u).Updates(map[string]interface{}{
		"password_hash": hash, "password_changed_at": now, "must_change_password": true,
	}).Error; err != nil {
		serverError(c, "Could not set the password", err)
		return
	}
	n := auth.RevokeSessions(u.ID, 0, "password reset by admin")
	audit(c, "password_reset", "user", u.ID, fmt.Sprintf("%s %s: temporary password set; %d session(s) signed out", u.UserNumber, u.Name, n))
	c.JSON(http.StatusOK, gin.H{"message": "Password set. They will choose a new one at their next sign-in."})
}

// ResetUser2FA: for a user who lost their phone. Their two-step sign-in is
// turned off and their sessions end; if the company requires it, they set
// it up again at their next sign-in.
func ResetUser2FA(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	database.DB.Model(&u).Updates(map[string]interface{}{"totp_enabled": false, "totp_secret": "", "totp_pending": "", "recovery_hashes": "", "totp_last_step": 0})
	n := auth.RevokeSessions(u.ID, 0, "two-step sign-in reset by admin")
	audit(c, "2fa_reset", "user", u.ID, fmt.Sprintf("%s %s: two-step sign-in reset; %d session(s) signed out", u.UserNumber, u.Name, n))
	c.JSON(http.StatusOK, gin.H{"message": "Two-step sign-in reset. They set it up again at their next sign-in."})
}

// SignOutUser ends all of a user's sessions.
func SignOutUser(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	var u models.User
	if err := database.DB.First(&u, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}
	n := auth.RevokeSessions(u.ID, auth.Session(c).ID, "signed out by admin")
	audit(c, "session_revoked", "user", u.ID, fmt.Sprintf("%s %s signed out everywhere by an admin (%d session(s))", u.UserNumber, u.Name, n))
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}
