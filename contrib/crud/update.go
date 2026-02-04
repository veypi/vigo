//
// update.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package crud

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/veypi/vigo"
	"gorm.io/gorm"
)

// Update 更新资源
func Update(db func() *gorm.DB, model any) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	return func(x *vigo.X) (any, error) {
		id := x.PathParams.Get("id")
		if id == "" {
			return nil, vigo.ErrArgMissing
		}

		// 查询现有记录
		existingModel := reflect.New(modelType).Interface()

		if err := db().First(existingModel, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, vigo.ErrNotFound
			}
			return nil, err
		}

		// 绑定更新数据
		updateData := make(map[string]any)
		if err := x.Parse(&updateData); err != nil {
			return nil, err
		}

		fmt.Printf("\n%v\n", updateData)
		// 更新记录
		if err := db().Model(existingModel).Updates(updateData).Error; err != nil {
			return nil, err
		}

		return existingModel, nil
	}
}

func Patch[T any](db GetDB) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf((*T)(nil)).Elem()
	if modelType.Kind() != reflect.Struct {
		panic("model must be a struct or pointer to struct")
	}
	fieldMap := make(map[string]reflect.StructField)

	for i := 0; i < modelType.NumField(); i++ {
		field := modelType.Field(i)
		columnName := getColumnName(field)
		fieldMap[strings.ToLower(columnName)] = field
	}

	return func(x *vigo.X) (any, error) {
		// 过滤requestBody，只保留模型中存在的字段
		requestBody := make(map[string]any)
		err := x.Parse(&requestBody)
		if err != nil {
			return nil, err
		}
		validUpdates := make(map[string]interface{})
		for key, value := range requestBody {
			// 将key转换为小写以便匹配
			lowerKey := strings.ToLower(key)
			if field, exists := fieldMap[lowerKey]; exists {
				// 处理字段类型转换
				convertedValue, err := convertFieldType(value, field.Type)
				if err != nil {
					fmt.Printf("Warning: cannot convert field %s: %v\n", key, err)
					continue
				}
				validUpdates[key] = convertedValue
			}
		}

		// 5. 执行更新操作
		if len(validUpdates) == 0 {
			return nil, fmt.Errorf("no valid fields to update")
		}

		// 先查询当前记录以返回更新后的数据
		result, err := gorm.G[T](db()).Where("id = ?", x.PathParams.Get("id")).First(x.Context())
		if err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, fmt.Errorf("record not found for update")
			}
			return nil, err
		}

		// 执行更新
		rows := db().Model(result).Updates(validUpdates)
		if rows.Error != nil {
			return nil, rows.Error
		}

		return rows.RowsAffected, nil
	}
}
