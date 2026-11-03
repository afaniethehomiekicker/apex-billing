package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
)

// Many sign-in attempts sent at the same moment: only 5 get through.
func TestLoginLimiterBurst(t *testing.T) {
	l := NewLoginLimiter()
	var passed int32
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Begin("email:a@b.c", "ip:1.2.3.4") {
				atomic.AddInt32(&passed, 1)
			}
		}()
	}
	wg.Wait()
	if passed != maxFails {
		t.Fatalf("%d attempts got through, want %d", passed, maxFails)
	}
}

// A right password is taken back and doesn't count towards the block.
func TestLoginLimiterForgive(t *testing.T) {
	l := NewLoginLimiter()
	for i := 0; i < 20; i++ {
		if !l.Begin("email:x") {
			t.Fatalf("blocked after %d successful sign-ins", i)
		}
		l.Forgive("email:x")
	}
	for i := 0; i < maxFails; i++ {
		l.Begin("email:x")
	}
	if l.Begin("email:x") {
		t.Fatal("not blocked after 5 failures")
	}
}

func gateRequest(t *testing.T, method, path string, u *models.User) int {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) { c.Set("user", u); c.Next() })
	g := r.Group("", Gate(func() bool { return true }))
	h := func(c *gin.Context) { c.Status(http.StatusOK) }
	g.GET("/api/settings", h)
	g.PUT("/api/settings", h)
	g.GET("/api/me", h)
	g.GET("/api/clients", h)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader("{}")))
	return w.Code
}

// Before the gate is passed, settings can be read but not changed.
func TestGateSettings(t *testing.T) {
	noMFA := &models.User{Role: models.RoleAdmin}
	if c := gateRequest(t, "GET", "/api/settings", noMFA); c != 200 {
		t.Fatalf("GET settings: %d", c)
	}
	if c := gateRequest(t, "PUT", "/api/settings", noMFA); c != 403 {
		t.Fatalf("PUT settings before 2FA setup: %d, want 403", c)
	}
	if c := gateRequest(t, "GET", "/api/clients", noMFA); c != 403 {
		t.Fatalf("clients before 2FA setup: %d, want 403", c)
	}
	tmp := &models.User{Role: models.RoleAdmin, TOTPEnabled: true, MustChangePassword: true}
	if c := gateRequest(t, "PUT", "/api/settings", tmp); c != 403 {
		t.Fatalf("PUT settings with a temporary password: %d, want 403", c)
	}
	ok := &models.User{Role: models.RoleAdmin, TOTPEnabled: true}
	if c := gateRequest(t, "PUT", "/api/settings", ok); c != 200 {
		t.Fatalf("PUT settings after the gate: %d", c)
	}
}

// X-Forwarded-Host from a browser (not a trusted proxy) doesn't make a
// cross-site request look like it came from this site.
func TestSameOriginForwardedHost(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	defer gin.SetMode(gin.TestMode)
	r := gin.New()
	r.SetTrustedProxies(nil)
	r.Use(SameOrigin())
	r.POST("/api/x", func(c *gin.Context) { c.Status(200) })
	send := func(origin, fwdHost string) int {
		req := httptest.NewRequest("POST", "http://billing.local/api/x", nil)
		req.Header.Set("X-Requested-With", "apex-billing")
		req.Header.Set("Origin", origin)
		if fwdHost != "" {
			req.Header.Set("X-Forwarded-Host", fwdHost)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	if c := send("http://billing.local", ""); c != 200 {
		t.Fatalf("same origin: %d", c)
	}
	if c := send("http://evil.example", "evil.example"); c != 403 {
		t.Fatalf("spoofed X-Forwarded-Host: %d, want 403", c)
	}
}

// Recovery codes are stored with a keyed hash (not a plain SHA-256).
func TestRecoveryCodesKeyed(t *testing.T) {
	appKey = []byte("0123456789abcdef0123456789abcdef")
	codes, hashes := NewRecoveryCodes(3)
	for i, c := range codes {
		if hashes[i] != KeyedHash("recovery:"+c) || hashes[i] == HashToken("recovery:"+c) {
			t.Fatalf("code %d not stored with the keyed hash", i)
		}
	}
	other := KeyedHash("recovery:" + codes[0])
	appKey = []byte("another-key-another-key-another-")
	if KeyedHash("recovery:"+codes[0]) == other {
		t.Fatal("keyed hash doesn't depend on the key")
	}
}
