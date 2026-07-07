package test

import (
	"fmt"
	"testing"

	"github.com/zhaojiabo/bobobeads_server/internal/db"
	"github.com/zhaojiabo/bobobeads_server/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDBCounter int

func SetupTestDB(t *testing.T) {
	t.Helper()
	testDBCounter++
	dsn := fmt.Sprintf("file:testdb%d?mode=memory&cache=shared", testDBCounter)

	var err error
	db.DB, err = gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	sqlDB, err := db.DB.DB()
	if err == nil {
		sqlDB.SetMaxOpenConns(10)
	}

	err = db.DB.AutoMigrate(
		&model.User{},
		&model.Work{},
		&model.CommunityPost{},
		&model.Like{},
		&model.Favorite{},
		&model.Comment{},
		&model.Follow{},
		&model.Template{},
		&model.TemplateCategory{},
		&model.Order{},
		&model.Product{},
		&model.Subscription{},
		&model.CreditTransaction{},
		&model.CreditAccount{},
		&model.Invite{},
		&model.BeadColor{},
		&model.BoardSpec{},
		&model.Config{},
		&model.Feedback{},
		&model.Generation{},
		&model.MediaAsset{},
	)
	if err != nil {
		t.Fatalf("failed to migrate test db: %v", err)
	}
}
