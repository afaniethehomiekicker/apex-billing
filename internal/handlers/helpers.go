package handlers

import (
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"apex-billing/internal/auth"
	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// serverError logs the real error and answers with a plain message (no
// database details reach the browser).
func serverError(c *gin.Context, msg string, err error) {
	log.Printf("%s %s: %s: %v", c.Request.Method, c.FullPath(), msg, err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": msg})
}

func badRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": msg})
}

// trimTo trims s and reports whether it fits in max characters.
func trimTo(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	return s, len([]rune(s)) <= max
}

// audit writes one audit-log row. Failure to log never fails the request.
func audit(c *gin.Context, action, entityType string, entityID uint, details string) {
	u := auth.User(c)
	row := models.AuditLog{
		UserID: u.ID, UserName: u.Name, Action: action, EntityType: entityType,
		EntityID: entityID, Details: details, IP: c.ClientIP(),
	}
	if err := database.DB.Create(&row).Error; err != nil {
		log.Printf("audit: %v", err)
	}
}

// idParam parses a numeric path parameter; answers 400 when invalid.
func idParam(c *gin.Context, name string) (uint, bool) {
	n, err := strconv.ParseUint(c.Param(name), 10, 64)
	if err != nil || n == 0 {
		badRequest(c, "Invalid id")
		return 0, false
	}
	return uint(n), true
}

// pageParams: ?page= (from 1) and ?page_size= (1–200, default 50).
func pageParams(c *gin.Context) (page, size int) {
	page, _ = strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ = strconv.Atoi(c.DefaultQuery("page_size", "50"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 50
	}
	return
}

// likeTerm makes a safe ILIKE pattern from search text.
func likeTerm(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 100 {
		s = string(r[:100])
	}
	s = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(strings.ToLower(s))
	return "%" + s + "%"
}

func roundMoney(v float64) float64 { return math.Round(v*100) / 100 }

func badNumber(v float64) bool { return math.IsNaN(v) || math.IsInf(v, 0) }

// businessZone: dates ("today", start / end dates) follow the company's
// clock, not the server's. DB_TIMEZONE, default Asia/Karachi. Read on first
// use (after .env is loaded).
var (
	zoneOnce sync.Once
	zone     *time.Location
)

func businessZone() *time.Location {
	zoneOnce.Do(func() {
		name := strings.TrimSpace(os.Getenv("DB_TIMEZONE"))
		if name == "" {
			name = "Asia/Karachi"
		}
		if loc, err := time.LoadLocation(name); err == nil {
			zone = loc
			return
		}
		// No time-zone database on this machine (some Windows installs):
		// Pakistan has no daylight saving, so a fixed +05:00 is exact.
		zone = time.FixedZone("PKT", 5*60*60)
	})
	return zone
}

// todayDate is today's date in the business time zone, as midnight UTC (the
// way date columns are read back); today is the same as "YYYY-MM-DD".
func todayDate() time.Time {
	y, m, d := time.Now().In(businessZone()).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func today() string { return todayDate().Format("2006-01-02") }

func dayString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// parseDay reads "YYYY-MM-DD" (a longer timestamp is cut to the date).
func parseDay(s string) (*time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	if len(s) > 10 {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// changeList describes what changed, for the audit log: "Name: A → B; ...".
type changeList []string

func (l *changeList) add(field, old, new string) {
	if old != new {
		if old == "" {
			old = "—"
		}
		if new == "" {
			new = "—"
		}
		*l = append(*l, fmt.Sprintf("%s: %s → %s", field, old, new))
	}
}

func (l changeList) String() string { return strings.Join(l, "; ") }

// errAnswered: inside a transaction, the handler has already answered (e.g.
// "not found"); roll back without another reply.
var errAnswered = errors.New("answered")

// auditTx writes an audit row inside a transaction (kept only if it commits).
func auditTx(tx *gorm.DB, c *gin.Context, action, entityType string, entityID uint, details string) {
	u := auth.User(c)
	row := models.AuditLog{
		UserID: u.ID, UserName: u.Name, Action: action, EntityType: entityType,
		EntityID: entityID, Details: details, IP: c.ClientIP(),
	}
	if err := tx.Create(&row).Error; err != nil {
		log.Printf("audit: %v", err)
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
