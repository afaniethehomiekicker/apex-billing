package handlers

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"
	"apex-billing/internal/qr"

	"github.com/gin-gonic/gin"
)

// Signing in and the signed-in user's own security.
//
//	POST   /api/auth/login              {email, password} → signed in, or {mfa_required, mfa_ticket}
//	POST   /api/auth/mfa                {mfa_ticket, code}  code = 6 digits or a recovery code
//	POST   /api/auth/logout
//	GET    /api/me
//	PUT    /api/me/password             {current_password, new_password}  (other sessions end)
//	GET    /api/me/sessions             where I'm signed in
//	DELETE /api/me/sessions/:id         sign that one out
//	POST   /api/me/sessions/revoke-others
//	POST   /api/me/2fa/setup            → {secret, uri, qr_svg}
//	POST   /api/me/2fa/enable           {code} → {recovery_codes}
//	POST   /api/me/2fa/disable          {password, code}
//	POST   /api/me/2fa/recovery-codes   {password, code} → new recovery codes

var loginLimiter = auth.NewLoginLimiter()

type meView struct {
	models.User
	RoleLabel        string   `json:"role_label"`
	Permissions      []string `json:"permissions"`
	MFASetupRequired bool     `json:"mfa_setup_required"`
	RecoveryLeft     int      `json:"recovery_codes_left"`
	HasAvatar        bool     `json:"has_avatar"`
	AvatarVersion    string   `json:"avatar_version"` // changes when the picture does
}

func me(u *models.User) meView {
	v := ""
	if u.AvatarPath != "" {
		v = auth.HashToken(u.AvatarPath)[:12]
	}
	return meView{User: *u, RoleLabel: models.Roles[u.Role], Permissions: auth.Permissions(u.Role),
		MFASetupRequired: !u.TOTPEnabled && require2FA(), RecoveryLeft: len(recoveryHashes(u)),
		HasAvatar: u.AvatarPath != "", AvatarVersion: v}
}

// require2FA: the company setting (read fresh; it's one small row).
func require2FA() bool {
	var on bool
	database.DB.Model(&models.CompanySettings{}).Where("id = 1").Select("require_2fa").Scan(&on)
	return on
}

// Require2FA is used by the auth gate.
func Require2FA() bool { return require2FA() }

func recoveryHashes(u *models.User) []string {
	var h []string
	if u.RecoveryHashes != "" {
		json.Unmarshal([]byte(u.RecoveryHashes), &h)
	}
	return h
}

// auditAs writes an audit row for a user who isn't signed in yet.
func auditAs(c *gin.Context, u *models.User, action, details string) {
	row := models.AuditLog{Action: action, EntityType: "user", Details: details, IP: c.ClientIP()}
	if u != nil {
		row.UserID, row.UserName, row.EntityID = u.ID, u.Name, u.ID
	}
	database.DB.Create(&row)
}

func signedIn(c *gin.Context, u *models.User, how string) {
	if _, err := auth.StartSession(c, u); err != nil {
		serverError(c, "Could not sign in", err)
		return
	}
	now := time.Now()
	database.DB.Model(u).Update("last_login_at", now)
	u.LastLoginAt = &now
	auditAs(c, u, "login", "Signed in"+how+" from "+c.ClientIP())
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

func Login(c *gin.Context) {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if email == "" || in.Password == "" || len(email) > 200 || len(in.Password) > 200 {
		badRequest(c, "Enter your email and password")
		return
	}
	keys := []string{"email:" + email, "ip:" + c.ClientIP()}
	// Counted before the password is checked (and taken back if it's
	// right), so a burst of requests sent at once can't get past the limit.
	if !loginLimiter.Begin(keys...) {
		auditAs(c, nil, "login_blocked", "Too many failed sign-ins for "+email+" / "+c.ClientIP())
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many failed sign-ins. Try again in 15 minutes."})
		return
	}
	var u models.User
	found := database.DB.Where("email = ?", email).First(&u).Error == nil
	var up *models.User
	if found {
		up = &u
	}
	// Same time spent whether or not the email exists.
	if !auth.CheckPasswordOrDummy(up, in.Password) {
		auditAs(c, up, "login_failed", "Wrong password for "+email)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Wrong email or password"})
		return
	}
	loginLimiter.Forgive(keys...)
	if !u.Active {
		auditAs(c, &u, "login_failed", "Disabled account")
		c.JSON(http.StatusForbidden, gin.H{"error": "This account is disabled. Ask an admin."})
		return
	}
	// Passwords stored with an older, cheaper setting are re-hashed now,
	// so every account takes the same time to check.
	if auth.NeedsRehash(u.PasswordHash) {
		if h, err := auth.HashPassword(in.Password); err == nil {
			database.DB.Model(&u).Update("password_hash", h)
			u.PasswordHash = h
		}
	}
	if u.TOTPEnabled {
		c.JSON(http.StatusOK, gin.H{"mfa_required": true, "mfa_ticket": auth.NewMFATicket(u.ID)})
		return
	}
	loginLimiter.Reset(keys...)
	signedIn(c, &u, "")
}

// checkSecondFactor: a code from the app (not reused), or an unused
// recovery code (then used up). Saves the user's 2FA state when it changes.
//
// Using up a code is one conditional UPDATE, so the same code sent twice at
// the same moment is accepted only once.
func checkSecondFactor(u *models.User, code string) (bool, string) {
	secret, err := auth.Open(u.TOTPSecret)
	if err == nil {
		if step, ok := auth.CheckTOTP(secret, code, u.TOTPLastStep, time.Now()); ok {
			res := database.DB.Model(&models.User{}).Where("id = ? AND totp_last_step < ?", u.ID, step).
				Update("totp_last_step", step)
			if res.Error != nil || res.RowsAffected != 1 {
				return false, ""
			}
			u.TOTPLastStep = step
			return true, ""
		}
	}
	if rc := auth.NormalizeRecovery(code); rc != "" {
		// Codes made before keyed hashes were used are still accepted.
		want := []string{auth.KeyedHash("recovery:" + rc), auth.HashToken("recovery:" + rc)}
		old := u.RecoveryHashes
		list := recoveryHashes(u)
		for i, x := range list {
			if subtle.ConstantTimeCompare([]byte(x), []byte(want[0])) == 1 || subtle.ConstantTimeCompare([]byte(x), []byte(want[1])) == 1 {
				list = append(list[:i], list[i+1:]...)
				b, _ := json.Marshal(list)
				res := database.DB.Model(&models.User{}).Where("id = ? AND recovery_hashes = ?", u.ID, old).
					Update("recovery_hashes", string(b))
				if res.Error != nil || res.RowsAffected != 1 {
					return false, ""
				}
				u.RecoveryHashes = string(b)
				return true, " with a recovery code"
			}
		}
	}
	return false, ""
}

func LoginMFA(c *gin.Context) {
	var in struct {
		Ticket string `json:"mfa_ticket"`
		Code   string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil || len(in.Code) > 20 {
		badRequest(c, "Invalid request")
		return
	}
	uid := auth.UseMFATicket(in.Ticket)
	if uid == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "That took too long or too many wrong codes. Sign in again.", "restart": true})
		return
	}
	var u models.User
	if database.DB.First(&u, uid).Error != nil || !u.Active || !u.TOTPEnabled {
		auth.DropMFATicket(in.Ticket)
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Sign in again.", "restart": true})
		return
	}
	keys := []string{"email:" + u.Email, "ip:" + c.ClientIP()}
	if !loginLimiter.Begin(keys...) {
		auth.DropMFATicket(in.Ticket)
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many failed sign-ins. Try again in 15 minutes.", "restart": true})
		return
	}
	ok, how := checkSecondFactor(&u, in.Code)
	if !ok {
		auditAs(c, &u, "login_failed", "Wrong two-step code")
		c.JSON(http.StatusUnauthorized, gin.H{"error": "That code isn't right. Check the time on your phone and try the newest code."})
		return
	}
	auth.DropMFATicket(in.Ticket)
	loginLimiter.Reset(keys...)
	signedIn(c, &u, how)
	if how != "" && len(recoveryHashes(&u)) <= 2 {
		auditAs(c, &u, "recovery_codes_low", fmt.Sprintf("%d recovery code(s) left", len(recoveryHashes(&u))))
	}
}

func Logout(c *gin.Context) {
	s := auth.Session(c)
	if s.ID != 0 {
		database.DB.Model(s).Updates(map[string]interface{}{"revoked_at": time.Now(), "revoke_reason": "signed out"})
		audit(c, "logout", "user", auth.User(c).ID, "Signed out")
	}
	auth.ClearCookie(c)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func GetMe(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": me(auth.User(c)), "idle_minutes": int(auth.IdleTimeout.Minutes())})
}

// ChangeMyPassword: the current password, then the new one. The user's
// other sessions end; this one continues.
func ChangeMyPassword(c *gin.Context) {
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	// Wrong current passwords count like wrong sign-ins, so someone using an
	// unattended signed-in screen can't keep guessing the password.
	keys := []string{"email:" + u.Email, "ip:" + c.ClientIP()}
	if !loginLimiter.Begin(keys...) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many wrong attempts. Try again in 15 minutes."})
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.Current) {
		auditAs(c, u, "password_change_failed", "Wrong current password")
		badRequest(c, "Your current password is not right")
		return
	}
	loginLimiter.Forgive(keys...)
	if msg := auth.PasswordProblem(in.New, u.Email, u.Name); msg != "" {
		badRequest(c, msg)
		return
	}
	if in.New == in.Current {
		badRequest(c, "Choose a password different from the current one")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		serverError(c, "Could not change the password", err)
		return
	}
	now := time.Now()
	if err := database.DB.Model(u).Updates(map[string]interface{}{
		"password_hash": hash, "password_changed_at": now, "must_change_password": false,
	}).Error; err != nil {
		serverError(c, "Could not change the password", err)
		return
	}
	u.PasswordHash, u.PasswordChangedAt, u.MustChangePassword = hash, &now, false
	n := auth.RevokeSessions(u.ID, auth.Session(c).ID, "password changed")
	audit(c, "password_changed", "user", u.ID, fmt.Sprintf("Changed own password; %d other session(s) signed out", n))
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

// ---- sessions ---------------------------------------------------------------------

func GetMySessions(c *gin.Context) {
	var list []models.Session
	database.DB.Where("user_id = ? AND revoked_at IS NULL AND expires_at > ? AND last_seen_at > ?", auth.User(c).ID, time.Now(),
		time.Now().Add(-auth.IdleTimeout)).Order("last_seen_at DESC").Limit(50).Find(&list)
	type row struct {
		models.Session
		Current bool `json:"current"`
	}
	out := []row{}
	cur := auth.Session(c).ID
	for _, s := range list {
		out = append(out, row{s, s.ID == cur})
	}
	var recent []models.AuditLog
	database.DB.Where("entity_type = 'user' AND entity_id = ? AND action IN ?", auth.User(c).ID,
		[]string{"login", "login_failed", "login_blocked", "logout"}).Order("id DESC").Limit(15).Find(&recent)
	c.JSON(http.StatusOK, gin.H{"sessions": out, "recent": recent})
}

func RevokeMySession(c *gin.Context) {
	id, ok := idParam(c, "id")
	if !ok {
		return
	}
	res := database.DB.Model(&models.Session{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL", id, auth.User(c).ID).
		Updates(map[string]interface{}{"revoked_at": time.Now(), "revoke_reason": "signed out by user"})
	if res.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Session not found"})
		return
	}
	audit(c, "session_revoked", "user", auth.User(c).ID, fmt.Sprintf("Signed out session #%d", id))
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func RevokeMyOtherSessions(c *gin.Context) {
	n := auth.RevokeSessions(auth.User(c).ID, auth.Session(c).ID, "signed out by user")
	audit(c, "session_revoked", "user", auth.User(c).ID, fmt.Sprintf("Signed out everywhere else (%d session(s))", n))
	c.JSON(http.StatusOK, gin.H{"revoked": n})
}

// ---- two-step sign-in setup ----------------------------------------------------------

func Setup2FA(c *gin.Context) {
	u := auth.User(c)
	if u.TOTPEnabled {
		badRequest(c, "Two-step sign-in is already on")
		return
	}
	secret := auth.NewTOTPSecret()
	sealed, err := auth.Seal(secret)
	if err != nil {
		serverError(c, "Could not start the setup", err)
		return
	}
	if err := database.DB.Model(u).Update("totp_pending", sealed).Error; err != nil {
		serverError(c, "Could not start the setup", err)
		return
	}
	issuer := "APEX Billing"
	if co := companySettings(); co.CompanyName != "" {
		issuer = "APEX Billing (" + co.CompanyName + ")"
	}
	uri := auth.TOTPURI(secret, u.Email, issuer)
	svg, err := qr.SVG(uri)
	if err != nil {
		serverError(c, "Could not make the QR code", err)
		return
	}
	// Grouped for typing: ABCD EFGH …
	var grouped []string
	for i := 0; i < len(secret); i += 4 {
		j := i + 4
		if j > len(secret) {
			j = len(secret)
		}
		grouped = append(grouped, secret[i:j])
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"secret": strings.Join(grouped, " "), "uri": uri, "qr_svg": svg})
}

func newRecovery(u *models.User) ([]string, error) {
	codes, hashes := auth.NewRecoveryCodes(10)
	b, _ := json.Marshal(hashes)
	u.RecoveryHashes = string(b)
	return codes, database.DB.Model(u).Update("recovery_hashes", u.RecoveryHashes).Error
}

func Enable2FA(c *gin.Context) {
	var in struct {
		Code string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	if u.TOTPEnabled {
		badRequest(c, "Two-step sign-in is already on")
		return
	}
	secret, err := auth.Open(u.TOTPPending)
	if u.TOTPPending == "" || err != nil {
		badRequest(c, "Start the setup again")
		return
	}
	step, ok := auth.CheckTOTP(secret, in.Code, 0, time.Now())
	if !ok {
		badRequest(c, "That code isn't right. Check the time on your phone and enter the newest code.")
		return
	}
	if err := database.DB.Model(u).Updates(map[string]interface{}{
		"totp_enabled": true, "totp_secret": u.TOTPPending, "totp_pending": "", "totp_last_step": step,
	}).Error; err != nil {
		serverError(c, "Could not turn on two-step sign-in", err)
		return
	}
	u.TOTPEnabled, u.TOTPSecret, u.TOTPPending, u.TOTPLastStep = true, u.TOTPPending, "", step
	codes, err := newRecovery(u)
	if err != nil {
		serverError(c, "Could not make recovery codes", err)
		return
	}
	n := auth.RevokeSessions(u.ID, auth.Session(c).ID, "two-step sign-in turned on")
	audit(c, "2fa_enabled", "user", u.ID, fmt.Sprintf("Two-step sign-in turned on; %d other session(s) signed out", n))
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"user": me(u), "recovery_codes": codes})
}

// confirmIdentity: password and a current code (or recovery code).
func confirmIdentity(c *gin.Context, u *models.User, password, code string) bool {
	keys := []string{"email:" + u.Email, "ip:" + c.ClientIP()}
	if !loginLimiter.Begin(keys...) {
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many wrong attempts. Try again in 15 minutes."})
		return false
	}
	if !auth.CheckPassword(u.PasswordHash, password) {
		badRequest(c, "Your password is not right")
		return false
	}
	if ok, _ := checkSecondFactor(u, code); !ok {
		badRequest(c, "The code isn't right")
		return false
	}
	loginLimiter.Forgive(keys...)
	return true
}

func Disable2FA(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	if !u.TOTPEnabled {
		badRequest(c, "Two-step sign-in is not on")
		return
	}
	if require2FA() {
		badRequest(c, "Your company requires two-step sign-in, so it can't be turned off. An admin can reset it if you lost your phone.")
		return
	}
	if !confirmIdentity(c, u, in.Password, in.Code) {
		return
	}
	database.DB.Model(u).Updates(map[string]interface{}{"totp_enabled": false, "totp_secret": "", "totp_pending": "", "recovery_hashes": ""})
	u.TOTPEnabled = false
	audit(c, "2fa_disabled", "user", u.ID, "Two-step sign-in turned off")
	c.JSON(http.StatusOK, gin.H{"user": me(u)})
}

func NewRecoveryCodes(c *gin.Context) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := c.ShouldBindJSON(&in); err != nil {
		badRequest(c, "Invalid request")
		return
	}
	u := auth.User(c)
	if !u.TOTPEnabled {
		badRequest(c, "Two-step sign-in is not on")
		return
	}
	if !confirmIdentity(c, u, in.Password, in.Code) {
		return
	}
	codes, err := newRecovery(u)
	if err != nil {
		serverError(c, "Could not make recovery codes", err)
		return
	}
	audit(c, "recovery_codes", "user", u.ID, "New recovery codes made (old ones no longer work)")
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"recovery_codes": codes, "user": me(u)})
}
