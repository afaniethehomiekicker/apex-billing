package handlers

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"
)

// Startup tasks. All of them are safe to run on every start.

// EnsureCounters raises each permanent-number counter to the highest number
// already used (never lowers it), and guards each number column with a
// unique index.
func EnsureCounters() {
	type source struct{ prefix, table, column string }
	sources := []source{
		{"BU", "users", "user_number"},
		{"CUS", "clients", "client_number"},
		{"SUB", "subscriptions", "subscription_number"},
		{"PAY", "payments", "payment_number"},
	}
	// Each company's invoice prefix.
	var cos []models.Company
	database.DB.Find(&cos)
	seen := map[string]bool{}
	for i := range cos {
		if p := invoicePrefixOf(&cos[i]); !seen[p] {
			seen[p] = true
			sources = append(sources, source{p, "invoices", "invoice_number"})
		}
	}
	if len(cos) == 0 {
		sources = append(sources, source{"INV", "invoices", "invoice_number"})
	}
	for _, s := range sources {
		var maxNum int64
		q := fmt.Sprintf(`SELECT COALESCE(MAX(CAST(SUBSTRING(%s FROM %d) AS BIGINT)), 0) FROM %s WHERE %s ~ ?`,
			s.column, len(s.prefix)+2, s.table, s.column)
		if err := database.DB.Raw(q, "^"+s.prefix+"-[0-9]+$").Scan(&maxNum).Error; err != nil {
			log.Printf("counters: reading %s: %v", s.prefix, err)
			continue
		}
		database.DB.Exec(`INSERT INTO id_counters (prefix, last) VALUES (?, ?)
			ON CONFLICT (prefix) DO UPDATE SET last = GREATEST(id_counters.last, EXCLUDED.last)`, s.prefix, maxNum)
		idx := "uniq_" + s.table + "_" + s.column
		database.DB.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS ` + idx + ` ON ` + s.table + ` (` + s.column + `) WHERE ` + s.column + ` <> ''`)
	}
}

// EnsureSettings creates the company settings row the first time.
func EnsureSettings() {
	var n int64
	database.DB.Model(&models.CompanySettings{}).Count(&n)
	if n == 0 {
		database.DB.Create(&models.CompanySettings{
			ID: 1, InvoicePrefix: "INV", Currency: "PKR", DefaultTermsDays: 15,
			CompanyName: strings.TrimSpace(os.Getenv("COMPANY_NAME")),
		})
	}
}

// EnsureFirstAdmin creates the first admin from BILLING_ADMIN_EMAIL /
// BILLING_ADMIN_PASSWORD when there are no users yet. That admin is asked to
// choose a new password at first sign-in.
//
// With no users nobody can sign in, so if the admin can't be created the
// program stops with the reason instead of starting unusable.
func EnsureFirstAdmin() {
	if HasUsers() {
		return
	}
	email := strings.ToLower(strings.Trim(strings.TrimSpace(os.Getenv("BILLING_ADMIN_EMAIL")), `"'`))
	pw := strings.Trim(strings.TrimSpace(os.Getenv("BILLING_ADMIN_PASSWORD")), `"'`)
	if email == "" || pw == "" {
		log.Fatal("No users yet: set BILLING_ADMIN_EMAIL and BILLING_ADMIN_PASSWORD in .env and start again to create the first admin.")
	}
	if !strings.Contains(email, "@") {
		log.Fatalf("BILLING_ADMIN_EMAIL %q is not an email address. The first admin was not created.", email)
	}
	// This password is temporary (it must be changed at first sign-in), so
	// the "must not contain your name or email" rule is not applied here;
	// length, letters + numbers, common and repetitive passwords still are.
	if msg := auth.PasswordProblem(pw, "", ""); msg != "" {
		log.Fatalf("BILLING_ADMIN_PASSWORD: %s. The first admin was not created; fix it in .env and start again.", msg)
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		log.Fatalf("first admin: %v", err)
	}
	now := time.Now()
	u := models.User{
		Name: "Administrator", Email: email, Role: models.RoleAdmin, Active: true,
		PasswordHash: hash, PasswordChangedAt: &now, MustChangePassword: true,
	}
	if err := database.DB.Create(&u).Error; err != nil {
		log.Fatalf("first admin: could not save it: %v", err)
	}
	log.Printf("First admin created: %s (choose a new password at first sign-in)", email)
}

// ResetAdmin is run from the server console with `apex-billing.exe --reset-admin`.
// It makes BILLING_ADMIN_EMAIL an active Admin with BILLING_ADMIN_PASSWORD as
// a temporary password (changed at next sign-in): it creates that user if it
// doesn't exist, otherwise it resets it — new password, two-step sign-in
// cleared, all its sessions ended. It lists the users and writes the audit log.
// Anyone who can run this can already read .env, so it gives nothing extra.
func ResetAdmin() {
	email := strings.ToLower(strings.Trim(strings.TrimSpace(os.Getenv("BILLING_ADMIN_EMAIL")), `"'`))
	pw := strings.Trim(strings.TrimSpace(os.Getenv("BILLING_ADMIN_PASSWORD")), `"'`)
	if email == "" || pw == "" || !strings.Contains(email, "@") {
		log.Fatal("--reset-admin: set BILLING_ADMIN_EMAIL and BILLING_ADMIN_PASSWORD in .env first.")
	}
	if msg := auth.PasswordProblem(pw, "", ""); msg != "" {
		log.Fatalf("--reset-admin: BILLING_ADMIN_PASSWORD: %s.", msg)
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		log.Fatalf("--reset-admin: %v", err)
	}

	var users []models.User
	database.DB.Order("id").Find(&users)
	fmt.Println("Users in the database:")
	if len(users) == 0 {
		fmt.Println("  (none)")
	}
	for _, u := range users {
		state := "active"
		if !u.Active {
			state = "disabled"
		}
		fmt.Printf("  %s  %-35s %-10s %s\n", u.UserNumber, u.Email, models.Roles[u.Role], state)
	}

	now := time.Now()
	var u models.User
	action, details := "", ""
	if database.DB.Where("lower(email) = ?", email).First(&u).Error == nil {
		err = database.DB.Model(&u).Updates(map[string]interface{}{
			"password_hash": hash, "password_changed_at": now, "must_change_password": true,
			"active": true, "role": models.RoleAdmin,
			"totp_enabled": false, "totp_secret": "", "totp_pending": "", "totp_last_step": 0, "recovery_hashes": "",
		}).Error
		if err != nil {
			log.Fatalf("--reset-admin: could not update %s: %v", email, err)
		}
		n := auth.RevokeSessions(u.ID, 0, "admin_reset")
		action = "admin_reset"
		details = fmt.Sprintf("%s %s reset from the server console: Admin, active, temporary password, two-step sign-in cleared; %d session(s) signed out", u.UserNumber, email, n)
	} else {
		u = models.User{
			Name: "Administrator", Email: email, Role: models.RoleAdmin, Active: true,
			PasswordHash: hash, PasswordChangedAt: &now, MustChangePassword: true,
		}
		if err := database.DB.Create(&u).Error; err != nil {
			log.Fatalf("--reset-admin: could not create %s: %v", email, err)
		}
		action = "created"
		details = fmt.Sprintf("%s %s created as Admin from the server console (--reset-admin)", u.UserNumber, email)
	}
	database.DB.Create(&models.AuditLog{
		UserName: "Server console", Action: action, EntityType: "user", EntityID: u.ID, Details: details, IP: "console",
	})
	fmt.Println()
	fmt.Println(details)
	fmt.Println("Now start apex-billing.exe normally, sign in as " + email + " with the password from .env,")
	fmt.Println("choose a new password, then remove BILLING_ADMIN_PASSWORD from .env.")
}

// HasUsers: at least one user exists.
func HasUsers() bool {
	var n int64
	database.DB.Model(&models.User{}).Count(&n)
	return n > 0
}

// EnsureDatabaseSafety: the audit log is append-only — the database itself
// refuses to change or delete its rows (safe to run on every start).
func EnsureDatabaseSafety() {
	stmts := []string{
		`CREATE OR REPLACE FUNCTION apex_audit_append_only() RETURNS trigger AS $$
		BEGIN RAISE EXCEPTION 'APEX Billing: audit log entries cannot be changed or deleted'; END $$ LANGUAGE plpgsql`,
		`DROP TRIGGER IF EXISTS audit_logs_append_only ON audit_logs`,
		`CREATE TRIGGER audit_logs_append_only BEFORE UPDATE OR DELETE ON audit_logs FOR EACH ROW EXECUTE FUNCTION apex_audit_append_only()`,
		`DROP TRIGGER IF EXISTS audit_logs_no_truncate ON audit_logs`,
		`CREATE TRIGGER audit_logs_no_truncate BEFORE TRUNCATE ON audit_logs FOR EACH STATEMENT EXECUTE FUNCTION apex_audit_append_only()`,
	}
	for _, q := range stmts {
		if err := database.DB.Exec(q).Error; err != nil {
			log.Printf("audit log protection: %v", err)
			return
		}
	}
}
