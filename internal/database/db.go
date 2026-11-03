package database

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB is the shared database handle.
var DB *gorm.DB

// Connect opens the PostgreSQL database named in the environment:
// DB_DSN, or DB_HOST / DB_PORT / DB_USER / DB_PASSWORD / DB_NAME.
// APEX Billing has its own database; it never touches APEX CORE's.
func Connect() {
	dsn := strings.TrimSpace(os.Getenv("DB_DSN"))
	if dsn == "" {
		get := func(k, def string) string {
			if v := strings.TrimSpace(os.Getenv(k)); v != "" {
				return v
			}
			return def
		}
		dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
			get("DB_HOST", "localhost"), get("DB_PORT", "5432"), get("DB_USER", "postgres"),
			os.Getenv("DB_PASSWORD"), get("DB_NAME", "apex_billing"), get("DB_SSLMODE", "disable"),
			get("DB_TIMEZONE", "Asia/Karachi"))
	}

	level := logger.Warn
	if strings.EqualFold(strings.TrimSpace(os.Getenv("DB_LOG")), "info") {
		level = logger.Info
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(level)})
	if err != nil {
		log.Fatalf("Could not connect to the database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Database handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	DB = db
	log.Println("Database connected")
}

// Migrate creates / updates every table. Called at startup.
func Migrate(models ...interface{}) {
	if err := DB.AutoMigrate(models...); err != nil {
		log.Fatalf("Database migration failed: %v", err)
	}
}
