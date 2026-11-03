package main

import (
	"crypto/tls"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/handlers"
	"apex-billing/internal/models"
	"apex-billing/internal/routes"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

// APEX Billing — a separate app for billing clients: its own users, its
// own database, no link to APEX CORE. One program: the API and the web
// pages (built into the binary from frontend/dist).

var buildVersion = "dev"

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found; using the environment")
	}
	if getEnv("GIN_MODE", "") == "" {
		os.Setenv("GIN_MODE", "release")
	}
	gin.SetMode(getEnv("GIN_MODE", "release"))
	auth.Init()

	database.Connect()
	database.Migrate(models.All()...)
	handlers.EnsureSettings()
	handlers.EnsureCompanies()
	handlers.EnsureCounters()
	for _, a := range os.Args[1:] {
		if a == "--reset-admin" || a == "-reset-admin" {
			handlers.EnsureDatabaseSafety()
			handlers.ResetAdmin()
			return
		}
	}
	handlers.EnsureFirstAdmin()
	handlers.EnsureDatabaseSafety()
	startupChecks()

	r := gin.New()
	if err := r.SetTrustedProxies(trustedProxies()); err != nil {
		log.Fatalf("Invalid TRUSTED_PROXIES: %v", err)
	}
	r.Use(gin.LoggerWithFormatter(func(p gin.LogFormatterParams) string {
		return p.TimeStamp.Format(time.RFC3339) + " | " + p.Method + " " + p.Path + " | " +
			http.StatusText(p.StatusCode) + " | " + p.ClientIP + "\n"
	}))
	r.Use(gin.Recovery())
	r.Use(securityHeaders(getEnv("TLS_ENABLED", "false") == "true"))
	r.Use(limitBody())
	r.MaxMultipartMemory = 8 << 20

	routes.Register(r)

	web, err := fs.Sub(frontendDist, "frontend/dist")
	if err != nil {
		log.Fatalf("Built frontend missing: %v (run the frontend build first)", err)
	}
	serveFrontend(r, web)

	port := getEnv("PORT", "8090")
	// Timeouts stop slow or stuck connections from tying the server up.
	srv := &http.Server{
		Addr:              getEnv("LISTEN_ADDR", "") + ":" + port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       90 * time.Second,
		WriteTimeout:      180 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10,
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
	}
	log.Printf("APEX Billing %s starting on %s", buildVersion, srv.Addr)
	if getEnv("TLS_ENABLED", "false") == "true" {
		if err := srv.ListenAndServeTLS(getEnv("TLS_CERT_FILE", "tls/cert.pem"), getEnv("TLS_KEY_FILE", "tls/key.pem")); err != nil {
			log.Fatalf("Server stopped: %v", err)
		}
		return
	}
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("Server stopped: %v", err)
	}
}

// startupChecks warns about settings that leave data exposed.
func startupChecks() {
	if getEnv("TLS_ENABLED", "false") != "true" && getEnv("TRUSTED_PROXIES", "") == "" && getEnv("COOKIE_SECURE", "") != "true" {
		log.Println("WARNING: running WITHOUT HTTPS. Passwords and data cross the network unencrypted. Use it only on this computer (LISTEN_ADDR=127.0.0.1), or turn on TLS_ENABLED / put it behind an HTTPS proxy. See SECURITY.md.")
	}
	if u := strings.ToLower(getEnv("DB_USER", "postgres")); u == "postgres" && getEnv("DB_DSN", "") == "" {
		log.Println("WARNING: connecting to the database as 'postgres' (full control of every database). Create a limited user with scripts/db-setup.sql. See SECURITY.md.")
	}
	if getEnv("BILLING_ADMIN_PASSWORD", "") != "" && handlers.HasUsers() {
		log.Println("WARNING: BILLING_ADMIN_PASSWORD is still in .env. It is no longer used; remove it.")
	}
	if getEnv("GIN_MODE", "release") != "release" {
		log.Println("WARNING: GIN_MODE is not 'release' (development mode).")
	}
}

// limitBody caps request sizes: 1 MB, 10 MB for a payment with its slip,
// 21 MB for a signed contract.
func limitBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		limit := int64(1 << 20)
		if c.Request.Method == http.MethodPost && c.Request.URL.Path == "/api/payments" {
			limit = 10 << 20
		}
		// A company logo up to 2 MB.
		if c.Request.Method == http.MethodPost && strings.HasPrefix(c.Request.URL.Path, "/api/companies/") &&
			strings.HasSuffix(c.Request.URL.Path, "/logo") {
			limit = 3 << 20
		}
		// A profile picture up to 2 MB.
		if c.Request.Method == http.MethodPost && strings.HasSuffix(c.Request.URL.Path, "/avatar") {
			limit = 3 << 20
		}
		// A signed contract (PDF / scan) up to 20 MB.
		if c.Request.Method == http.MethodPost && strings.HasPrefix(c.Request.URL.Path, "/api/contracts/") &&
			strings.HasSuffix(c.Request.URL.Path, "/document") {
			limit = 21 << 20
		}
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "The request is too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func trustedProxies() []string {
	var out []string
	for _, p := range strings.Split(os.Getenv("TRUSTED_PROXIES"), ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Content-Security-Policy: only this server's own scripts, styles, images
// and connections; no plugins, no framing by other sites.
const csp = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; " +
	"font-src 'self' data:; connect-src 'self'; frame-src 'self' blob:; worker-src 'self' blob:; object-src 'none'; " +
	"base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

func securityHeaders(tls bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", csp)
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			// Billing data is never kept in browser or proxy caches.
			h.Set("Cache-Control", "no-store")
		}
		// HTTPS served here, or by a trusted proxy in front.
		if tls || auth.IsSecure(c) {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

// serveFrontend serves the built web pages; any path that isn't an API
// call or a real file gets index.html, so the pages handle it.
func serveFrontend(r *gin.Engine, web fs.FS) {
	fileServer := http.FileServer(http.FS(web))
	index, err := fs.ReadFile(web, "index.html")
	if err != nil {
		log.Fatalf("index.html missing in the frontend build: %v", err)
	}
	isAPI := func(p string) bool { return p == "/api" || strings.HasPrefix(p, "/api/") }
	r.Use(func(c *gin.Context) {
		if isAPI(c.Request.URL.Path) {
			c.Next()
			return
		}
		if f, err := web.Open(strings.TrimPrefix(c.Request.URL.Path, "/")); err == nil {
			f.Close()
			fileServer.ServeHTTP(c.Writer, c.Request)
			c.Abort()
			return
		}
		c.Next()
	})
	r.NoRoute(func(c *gin.Context) {
		if isAPI(c.Request.URL.Path) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Not found"})
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}
