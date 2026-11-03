package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// Sign-in for APEX Billing.
//
// A session is a random token kept in an HttpOnly cookie (page scripts
// can't read it), SameSite=Strict, Secure on HTTPS. The server keeps only
// a SHA-256 of the token, so a copy of the database can't be used to sign
// in. Sessions end on sign-out, after SESSION_IDLE_MINUTES without use
// (default 30), after SESSION_MAX_HOURS (default 12), and when the user is
// disabled, has their password reset or changes it (their other sessions).
// Every request re-reads the session and the user.

const (
	CookieName     = "apex_session"
	MinPasswordLen = 10
	csrfHeader     = "X-Requested-With"
	csrfValue      = "apex-billing"
)

func envInt(key string, def, min, max int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key))); err == nil && v >= min && v <= max {
		return v
	}
	return def
}

// IdleTimeout and MaxLifetime are read once at startup (Init).
var (
	IdleTimeout = 30 * time.Minute
	MaxLifetime = 12 * time.Hour
	secureMode  = "auto" // COOKIE_SECURE: auto | true | false
	appKey      []byte
)

// Init reads the settings after .env is loaded. APP_SECRET (or the older
// JWT_SECRET) protects stored two-step sign-in keys; in release mode the
// program refuses to start without a long one.
func Init() {
	IdleTimeout = time.Duration(envInt("SESSION_IDLE_MINUTES", 30, 5, 24*60)) * time.Minute
	MaxLifetime = time.Duration(envInt("SESSION_MAX_HOURS", 12, 1, 24*14)) * time.Hour
	switch strings.ToLower(strings.TrimSpace(os.Getenv("COOKIE_SECURE"))) {
	case "true", "1", "yes":
		secureMode = "true"
	case "false", "0", "no":
		secureMode = "false"
	}
	secret := strings.TrimSpace(os.Getenv("APP_SECRET"))
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv("JWT_SECRET"))
	}
	release := os.Getenv("GIN_MODE") == "release"
	weak := len(secret) < 32 || strings.Contains(strings.ToLower(secret), "change-me")
	switch {
	case weak && release:
		log.Fatal("APP_SECRET is missing or weak: set a random value of at least 32 characters in .env (e.g. run: openssl rand -hex 32). Refusing to start.")
	case secret == "":
		buf := make([]byte, 32)
		rand.Read(buf)
		secret = hex.EncodeToString(buf)
		log.Println("WARNING: APP_SECRET is not set (development only). Two-step sign-in set up now will stop working after a restart.")
	case weak:
		log.Println("WARNING: APP_SECRET is weak; use a random value of at least 32 characters.")
	}
	k := sha256.Sum256([]byte("apex-billing/v1/" + secret))
	appKey = k[:]
}

// ---- secrets at rest -----------------------------------------------------------

// Seal encrypts a small secret (AES-256-GCM) for storing in the database.
func Seal(plain string) (string, error) {
	block, err := aes.NewCipher(appKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	return base64.RawStdEncoding.EncodeToString(gcm.Seal(nonce, nonce, []byte(plain), nil)), nil
}

// Open decrypts what Seal made.
func Open(sealed string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(appKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed value too short")
	}
	out, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", errors.New("can't decrypt (was APP_SECRET changed?)")
	}
	return string(out), nil
}

// RandomToken: n random bytes, URL-safe text.
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func HashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

// KeyedHash: HMAC-SHA256 with the key from APP_SECRET. Used for recovery
// codes: they are short, so a plain hash in a stolen copy of the database
// could be guessed offline; with the key that needs APP_SECRET too.
func KeyedHash(t string) string {
	m := hmac.New(sha256.New, appKey)
	m.Write([]byte(t))
	return "k1:" + hex.EncodeToString(m.Sum(nil))
}

// ---- passwords -----------------------------------------------------------------

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), 12)
	return string(b), err
}

// NeedsRehash: stored with a lower bcrypt cost than new passwords get.
func NeedsRehash(hash string) bool {
	c, err := bcrypt.Cost([]byte(hash))
	return err == nil && c < 12
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// dummyHash: compared against when the email is unknown, so a wrong email
// takes as long as a wrong password (no telling which emails exist).
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password-0"), 12)

func CheckPasswordOrDummy(u *models.User, pw string) bool {
	if u == nil || u.PasswordHash == "" {
		bcrypt.CompareHashAndPassword(dummyHash, []byte(pw))
		return false
	}
	return CheckPassword(u.PasswordHash, pw)
}

var commonPasswords = map[string]bool{}

func init() {
	for _, p := range strings.Fields(`password password1 password12 password123 password1234 passw0rd p@ssw0rd p@ssword
		123456 1234567 12345678 123456789 1234567890 12345678910 0123456789 987654321 qwerty123 qwerty1234 qwertyuiop
		1q2w3e4r 1q2w3e4r5t 1qaz2wsx zaq12wsx abc123 abcd1234 abc12345 admin123 admin1234 administrator welcome1
		welcome123 letmein1 iloveyou1 monkey123 dragon123 sunshine1 princess1 football1 baseball1 trustno1 master123
		pakistan1 pakistan123 pakistan786 karachi123 lahore123 islamabad1 786786 786786786 bismillah bismillah1
		bismillah786 apex1234 apexbilling billing123 changeme changeme1 changeme123 secret123 test1234 test12345`) {
		commonPasswords[p] = true
	}
}

// PasswordProblem returns a message when a new password is too weak. email
// and name (the person's) may be empty.
func PasswordProblem(pw, email, name string) string {
	if len([]rune(pw)) < MinPasswordLen {
		return "The password must be at least 10 characters"
	}
	if len(pw) > 72 {
		return "The password is too long (max 72 characters)"
	}
	hasLetter, hasDigit := false, false
	for _, r := range pw {
		switch {
		case r >= '0' && r <= '9':
			hasDigit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			hasLetter = true
		}
	}
	if !hasLetter || !hasDigit {
		return "The password must contain letters and numbers"
	}
	low := strings.ToLower(pw)
	if commonPasswords[low] || strings.Contains(low, "password") || strings.Contains(low, "qwerty") || strings.Contains(low, "12345") {
		return "That password is too common or easy to guess"
	}
	distinct := map[rune]bool{}
	for _, r := range low {
		distinct[r] = true
	}
	if len(distinct) < 5 {
		return "Use a less repetitive password"
	}
	parts := []string{}
	if i := strings.Index(email, "@"); i > 0 {
		parts = append(parts, strings.ToLower(email[:i]))
	}
	for _, f := range strings.Fields(strings.ToLower(name)) {
		parts = append(parts, f)
	}
	for _, p := range parts {
		if len([]rune(p)) >= 4 && strings.Contains(low, p) {
			return "The password must not contain your name or email"
		}
	}
	return ""
}

// ---- sessions -------------------------------------------------------------------

// isSecure: the browser is talking HTTPS (directly, or via a trusted proxy
// that says so).
// IsSecure is isSecure for other packages (e.g. to send HSTS behind a proxy).
func IsSecure(c *gin.Context) bool { return isSecure(c) }

func isSecure(c *gin.Context) bool {
	switch secureMode {
	case "true":
		return true
	case "false":
		return false
	}
	if c.Request.TLS != nil {
		return true
	}
	// X-Forwarded-Proto only counts when the request came through a trusted
	// proxy (gin's ClientIP differs from the direct peer then).
	if strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") && c.ClientIP() != c.RemoteIP() {
		return true
	}
	return false
}

func setCookie(c *gin.Context, value string, maxAge int) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name: CookieName, Value: value, Path: "/", MaxAge: maxAge,
		HttpOnly: true, Secure: isSecure(c), SameSite: http.SameSiteStrictMode,
	})
}

// StartSession creates a session for the user and sets the cookie.
func StartSession(c *gin.Context, u *models.User) (*models.Session, error) {
	token := RandomToken(32)
	now := time.Now()
	ua := c.Request.UserAgent()
	if len(ua) > 300 {
		ua = ua[:300]
	}
	s := models.Session{TokenHash: HashToken(token), UserID: u.ID, LastSeenAt: now, ExpiresAt: now.Add(MaxLifetime),
		IP: c.ClientIP(), UserAgent: ua}
	if err := database.DB.Create(&s).Error; err != nil {
		return nil, err
	}
	setCookie(c, token, int(MaxLifetime.Seconds()))
	return &s, nil
}

// ClearCookie removes the session cookie from the browser.
func ClearCookie(c *gin.Context) { setCookie(c, "", -1) }

// RevokeSessions ends a user's sessions (all, or all but one).
func RevokeSessions(userID uint, exceptID uint, reason string) int64 {
	q := database.DB.Model(&models.Session{}).Where("user_id = ? AND revoked_at IS NULL", userID)
	if exceptID != 0 {
		q = q.Where("id <> ?", exceptID)
	}
	res := q.Updates(map[string]interface{}{"revoked_at": time.Now(), "revoke_reason": reason})
	return res.RowsAffected
}

func endedJSON(c *gin.Context, msg string) {
	ClearCookie(c)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": msg})
}

// Required: a valid session. Puts the user ("user") and session
// ("session") in the context.
func Required() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(CookieName)
		if err != nil || token == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Please sign in"})
			return
		}
		var s models.Session
		if err := database.DB.Where("token_hash = ?", HashToken(token)).First(&s).Error; err != nil || s.RevokedAt != nil {
			endedJSON(c, "Your session has ended. Please sign in again.")
			return
		}
		now := time.Now()
		if now.After(s.ExpiresAt) {
			database.DB.Model(&s).Updates(map[string]interface{}{"revoked_at": now, "revoke_reason": "expired"})
			endedJSON(c, "Your session has ended. Please sign in again.")
			return
		}
		if now.Sub(s.LastSeenAt) > IdleTimeout {
			database.DB.Model(&s).Updates(map[string]interface{}{"revoked_at": now, "revoke_reason": "idle"})
			endedJSON(c, "You were signed out after "+strconv.Itoa(int(IdleTimeout.Minutes()))+" minutes without activity. Please sign in again.")
			return
		}
		var u models.User
		if err := database.DB.First(&u, s.UserID).Error; err != nil || !u.Active {
			database.DB.Model(&s).Updates(map[string]interface{}{"revoked_at": now, "revoke_reason": "user disabled"})
			endedJSON(c, "Your session has ended. Please sign in again.")
			return
		}
		if now.Sub(s.LastSeenAt) > time.Minute {
			database.DB.Model(&s).Update("last_seen_at", now)
			s.LastSeenAt = now
		}
		c.Set("user", &u)
		c.Set("session", &s)
		c.Next()
	}
}

// User returns the signed-in user (after Required).
func User(c *gin.Context) *models.User {
	if v, ok := c.Get("user"); ok {
		if u, ok := v.(*models.User); ok {
			return u
		}
	}
	return &models.User{}
}

// Session returns the current session (after Required).
func Session(c *gin.Context) *models.Session {
	if v, ok := c.Get("session"); ok {
		if s, ok := v.(*models.Session); ok {
			return s
		}
	}
	return &models.Session{}
}

// SameOrigin blocks cross-site requests that change things: they must
// carry the app's own header (a cross-site page can't add it without a
// CORS permission this server never gives) and, when the browser says
// where they came from, come from this site.
func SameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if c.GetHeader(csrfHeader) != csrfValue {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Request blocked (missing app header). Reload the page."})
			return
		}
		if site := c.GetHeader("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Cross-site request blocked"})
			return
		}
		// (The development page server on another port is allowed in debug mode.)
		if origin := c.GetHeader("Origin"); origin != "" && origin != "null" && gin.Mode() != gin.DebugMode {
			host := origin
			if i := strings.Index(host, "://"); i >= 0 {
				host = host[i+3:]
			}
			// X-Forwarded-Host only counts when a trusted proxy sent it.
			viaProxy := c.ClientIP() != c.RemoteIP()
			if !strings.EqualFold(host, c.Request.Host) && !(viaProxy && strings.EqualFold(host, c.GetHeader("X-Forwarded-Host"))) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Cross-site request blocked"})
				return
			}
		}
		c.Next()
	}
}

// Gate: until the user has chosen their own password, and (when the
// company requires it) set up two-step sign-in, only the pages for doing
// that work. Enforced here, not just in the browser.
func Gate(require2FA func() bool) gin.HandlerFunc {
	allowed := map[string]bool{
		"/api/me": true, "/api/me/password": true, "/api/auth/logout": true,
		"/api/me/2fa/setup": true, "/api/me/2fa/enable": true, "/api/settings": true,
	}
	return func(c *gin.Context) {
		u := User(c)
		// Settings may be read (the pages need them) but not changed until
		// the gate is passed: otherwise an admin who hasn't set up two-step
		// sign-in yet could switch "required for everyone" off.
		if allowed[c.FullPath()] && !(c.FullPath() == "/api/settings" && c.Request.Method != http.MethodGet) {
			c.Next()
			return
		}
		if u.MustChangePassword {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Choose a new password first", "code": "password_change_required"})
			return
		}
		if !u.TOTPEnabled && require2FA() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Set up two-step sign-in first", "code": "mfa_setup_required"})
			return
		}
		c.Next()
	}
}

// ---- pending two-step sign-ins -------------------------------------------------

// MFATicket: the password was right; the code is still needed. Kept in
// memory for 5 minutes, 5 tries.
type MFATicket struct {
	UserID   uint
	Expires  time.Time
	Attempts int
}

var (
	ticketsMu sync.Mutex
	tickets   = map[string]*MFATicket{}
)

func NewMFATicket(userID uint) string {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	now := time.Now()
	for k, t := range tickets {
		if now.After(t.Expires) {
			delete(tickets, k)
		}
	}
	id := RandomToken(24)
	tickets[id] = &MFATicket{UserID: userID, Expires: now.Add(5 * time.Minute)}
	return id
}

// UseMFATicket counts a try; returns the user id, or 0 when the ticket is
// unknown, expired or out of tries.
func UseMFATicket(id string) uint {
	ticketsMu.Lock()
	defer ticketsMu.Unlock()
	t := tickets[id]
	if t == nil || time.Now().After(t.Expires) || t.Attempts >= 5 {
		delete(tickets, id)
		return 0
	}
	t.Attempts++
	return t.UserID
}

func DropMFATicket(id string) {
	ticketsMu.Lock()
	delete(tickets, id)
	ticketsMu.Unlock()
}

// ---- permissions ------------------------------------------------------------

// What each role may do. Admin may do everything.
var rolePerms = map[string]map[string]bool{
	models.RoleAccountant: {"view_billing": true, "manage_clients": true, "manage_billing": true, "record_payments": true, "verify_payments": true, "view_reports": true},
	models.RoleRecorder:   {"view_billing": true, "record_payments": true},
	models.RoleVerifier:   {"view_billing": true, "verify_payments": true, "view_reports": true},
}

// Can reports whether a role has a permission:
//
//	view_billing     see clients, invoices and payments
//	manage_clients   add / edit / archive clients
//	manage_billing   products, services, SLAs, contracts, invoices
//	record_payments  record a payment with its slip
//	verify_payments  verify / reject / reverse payments
//	view_reports     reports and exports
//	manage_users     users (admin only)
//	manage_settings  company settings (admin only)
//	view_audit       the audit log (admin only)
func Can(role, perm string) bool {
	if role == models.RoleAdmin {
		return true
	}
	return rolePerms[role][perm]
}

// Permissions lists everything a role may do (for the browser).
func Permissions(role string) []string {
	all := []string{"view_billing", "manage_clients", "manage_billing", "record_payments", "verify_payments",
		"view_reports", "manage_users", "manage_settings", "view_audit"}
	var out []string
	for _, p := range all {
		if Can(role, p) {
			out = append(out, p)
		}
	}
	return out
}

// Require: the signed-in user's role must have the permission.
func Require(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !Can(User(c).Role, perm) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You don't have permission to do this"})
			return
		}
		c.Next()
	}
}

// RequireAny: the role must have at least one of the permissions.
func RequireAny(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role := User(c).Role
		for _, p := range perms {
			if Can(role, p) {
				c.Next()
				return
			}
		}
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "You don't have permission to do this"})
	}
}

// ---- sign-in rate limit -----------------------------------------------------

// LoginLimiter: after 5 failed sign-ins for the same email or from the same
// address in 15 minutes, further attempts are refused until the window ends.
type LoginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const (
	maxFails   = 5
	failWindow = 15 * time.Minute
)

func NewLoginLimiter() *LoginLimiter { return &LoginLimiter{fails: map[string][]time.Time{}} }

func (l *LoginLimiter) recent(k string, now time.Time) []time.Time {
	var keep []time.Time
	for _, t := range l.fails[k] {
		if now.Sub(t) < failWindow {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(l.fails, k)
	} else {
		l.fails[k] = keep
	}
	return keep
}

// Blocked reports whether either key has too many recent failures.
func (l *LoginLimiter) Blocked(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		if len(l.recent(k, now)) >= maxFails {
			return true
		}
	}
	return false
}

func (l *LoginLimiter) Fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		l.fails[k] = append(l.recent(k, now), now)
	}
}

// Begin checks and counts an attempt in one step, so many requests sent at
// the same moment can't all slip in before the first failure is counted.
// It returns false when blocked. A successful attempt is taken back with
// Forgive (or everything cleared with Reset).
func (l *LoginLimiter) Begin(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		if len(l.recent(k, now)) >= maxFails {
			return false
		}
	}
	for _, k := range keys {
		l.fails[k] = append(l.recent(k, now), now)
	}
	return true
}

// Forgive takes back the newest attempt counted by Begin.
func (l *LoginLimiter) Forgive(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		if f := l.fails[k]; len(f) > 0 {
			if len(f) == 1 {
				delete(l.fails, k)
			} else {
				l.fails[k] = f[:len(f)-1]
			}
		}
	}
}

func (l *LoginLimiter) Reset(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.fails, k)
	}
}
