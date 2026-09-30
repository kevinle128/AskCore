package database

import (
	"os"
	"strings"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"AskCore/internal/models"
)

var DB *gorm.DB

// InitDB initializes the database connection
func InitDB() (*gorm.DB, error) {
	var err error
	dsn := os.Getenv("DATABASE_URL")

	if dsn == "" {
		// Default to SQLite for development
		DB, err = gorm.Open(sqlite.Open("app.db"), &gorm.Config{})
	} else if strings.HasPrefix(dsn, "postgres") {
		DB, err = gorm.Open(postgres.Open(dsn), &gorm.Config{})
	} else {
		// Assume SQLite file path
		DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	}

	if err != nil {
		return nil, err
	}

	// Auto-migrate models
	err = DB.AutoMigrate(&models.User{}, &models.Post{})
	if err != nil {
		return nil, err
	}

	return DB, nil
}

// GetDB returns the database instance
func GetDB() *gorm.DB {
	return DB
}
