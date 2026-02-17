//
// db.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package config

import (
	"errors"
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

type Database struct {
	Type   string `json:"type"`
	DSN    string `json:"dsn"`
	client *gorm.DB
}

func (d *Database) Client() *gorm.DB {
	if d.client == nil {
		var dialect gorm.Dialector
		switch d.Type {
		case "mysql":
			dialect = mysql.Open(d.DSN)
		case "postgres":
			dialect = postgres.Open(d.DSN)
		case "sqlite", "sqlite3":
			dialect = sqlite.Open(d.DSN)
		default:
			panic(errors.New("不支持的数据库类型: " + d.Type))
		}
		var err error
		d.client, err = gorm.Open(dialect, &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		if err != nil {
			panic(fmt.Errorf("数据库连接失败: %w", err))
		}
	}
	return d.client
}

func (d *Database) SetClient(db *gorm.DB) {
	d.client = db
}
