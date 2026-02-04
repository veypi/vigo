//
// db.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package vigo

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Model struct {
	ID        string         `json:"id" gorm:"primaryKey;type:varchar(64);comment:ID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *Model) BeforeCreate(tx *gorm.DB) error {
	if m.ID == "" {
		m.ID = strings.ReplaceAll(uuid.New().String(), "-", "")
	}
	return nil
}

type ModelList struct {
	list []any
}

func (ms *ModelList) Add(model any) {
	ms.list = append(ms.list, model)
}

func (ms *ModelList) GetList() []any {
	return ms.list
}

func (ms *ModelList) Append(models ...any) {
	ms.list = append(ms.list, models...)
}

func (ms *ModelList) AutoMigrate(db *gorm.DB) error {
	items := make([]any, 0, 10)
	items = append(items, ms.list...)
	db.DisableForeignKeyConstraintWhenMigrating = true
	err := db.AutoMigrate(items...)
	if err != nil {
		return err
	}
	db.DisableForeignKeyConstraintWhenMigrating = false
	return db.AutoMigrate(items...)
}

func (ms *ModelList) AutoDrop(db *gorm.DB) error {
	fmt.Print("\ncontinue to drop db？(yes/no): ")
	// 读取用户输入
	reader := bufio.NewReader(os.Stdin)
	input, err := reader.ReadString('\n')
	if err != nil {
		return err
	}
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "yes" || input == "y" {
		items := make([]any, 0, 10)
		items = append(items, ms.list...)
		return db.Migrator().DropTable(items...)
	}
	return nil
}
