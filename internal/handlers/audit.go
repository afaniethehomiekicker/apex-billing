package handlers

import (
	"net/http"
	"strings"

	"apex-billing/internal/database"
	"apex-billing/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// GET /api/audit   ?entity_type= &q= &page= &page_size=   (view_audit)
func GetAuditLog(c *gin.Context) {
	q := database.DB.Model(&models.AuditLog{})
	if et := strings.TrimSpace(c.Query("entity_type")); et != "" {
		q = q.Where("entity_type = ?", et)
	}
	if s := strings.TrimSpace(c.Query("q")); s != "" {
		like := likeTerm(s)
		q = q.Where("(LOWER(details) LIKE ? OR LOWER(user_name) LIKE ? OR LOWER(action) LIKE ?)", like, like, like)
	}
	var total int64
	if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		serverError(c, "Could not load the audit log", err)
		return
	}
	page, size := pageParams(c)
	var rows []models.AuditLog
	if err := q.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&rows).Error; err != nil {
		serverError(c, "Could not load the audit log", err)
		return
	}
	if rows == nil {
		rows = []models.AuditLog{}
	}
	c.JSON(http.StatusOK, gin.H{"entries": rows, "total": total, "page": page, "page_size": size})
}
