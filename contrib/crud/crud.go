//
// crud.go
// Copyright (C) 2025 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package crud

import (
	"reflect"
	"strconv"
	"strings"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
	"gorm.io/gorm"
)

type GetDB = func() *gorm.DB

func All(r vigo.Router, db GetDB, target any) vigo.Router {
	r.Get("/{id}", "generated api", target, Get(db, target))
	r.Get("", "generated api", target, List(db, target))
	r.Post("", "generated api", target, Create(db, target))
	// r.Patch("/:id", "generated api", target, Update(db, target))
	// r.Patch("/:id", "generated api", target, Update(db, target))
	r.Delete("/:id", "generated api", target, Delete(db, target))
	return r
}

// Create 创建资源
func Create(db func() *gorm.DB, model any) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	return func(x *vigo.X) (any, error) {
		// 创建模型实例
		modelValue := reflect.New(modelType).Interface()
		// 绑定JSON数据
		if err := x.Parse(modelValue); err != nil {
			return nil, err
		}
		logv.Warn().Msgf("%v", modelValue)

		// 保存到数据库
		if err := db().Create(modelValue).Error; err != nil {
			return nil, err
		}
		return modelValue, nil
	}
}

// Get 根据ID获取资源
func Get(db func() *gorm.DB, model any) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	return func(x *vigo.X) (any, error) {
		id := x.PathParams.Get("id")
		if id == "" {
			return nil, vigo.ErrNotFound
		}

		modelValue := reflect.New(modelType).Interface()

		if err := db().First(modelValue, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, vigo.ErrNotFound
			}
			return nil, err
		}
		return modelValue, nil
	}
}

// PaginatedResponse 分页响应结构
type PaginatedResponse struct {
	Items      any   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	PageSize   int   `json:"page_size"`
	TotalPages int   `json:"total_pages"`
}

// GetAll 获取所有资源（支持分页、过滤和模糊查询）
func List(db func() *gorm.DB, model any) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}
	return func(x *vigo.X) (any, error) {
		// 创建切片类型
		sliceType := reflect.SliceOf(modelType)
		sliceValue := reflect.New(sliceType).Interface()

		// 解析URL查询参数
		queryParams := x.Request.URL.Query()
		params := make(map[string]any)

		// 将URL查询参数转换为map
		for key, values := range queryParams {
			if len(values) > 0 {
				params[key] = values[0] // 取第一个值
			}
		}

		// 提取分页参数
		page := 1
		pageSize := 10
		sort_by := ""
		order := "desc"

		if p, ok := params["page"]; ok {
			if pageStr, ok := p.(string); ok {
				if pageVal, err := strconv.Atoi(pageStr); err == nil && pageVal > 0 {
					page = pageVal
					delete(params, "page")
				}
			}
		}
		if ps, ok := params["sort_by"]; ok {
			if psStr, ok := ps.(string); ok {
				sort_by = psStr
				delete(params, "sort_by")
			}
		}

		if ps, ok := params["page_size"]; ok {
			if pageSizeStr, ok := ps.(string); ok {
				if pageSizeVal, err := strconv.Atoi(pageSizeStr); err == nil && pageSizeVal > 0 && pageSizeVal <= 100 {
					pageSize = pageSizeVal
					delete(params, "page_size")
				}
			}
		}
		if ps, ok := params["order"]; ok {
			if psStr, ok := ps.(string); ok {
				if psStr == "asc" || psStr == "desc" {
					order = psStr
				}
				delete(params, "order")
			}
		}

		offset := (page - 1) * pageSize
		logv.Warn().Msgf("%v", params)

		// 构建查询条件
		query := db().Model(reflect.New(modelType).Interface())
		if sort_by != "" {
			query = query.Order(sort_by + " " + order)
		}

		// 遍历参数构建查询条件

		// 检查字段是否为字符串类型，如果是则使用模糊查询
		modelValue := reflect.New(modelType).Interface()
		modelType := reflect.TypeOf(modelValue).Elem()

		// 查找对应的字段
		for i := 0; i < modelType.NumField(); i++ {
			field := modelType.Field(i)
			// 检查字段名或json标签
			fieldName := field.Name
			if jsonTag := field.Tag.Get("json"); jsonTag != "" && jsonTag != "-" {
				fieldName = strings.Split(jsonTag, ",")[0]
			}
			if jsonTag := field.Tag.Get("src"); strings.HasPrefix(jsonTag, "path") {
				if split := strings.Split(jsonTag, "@"); len(split) > 1 {
					fieldName = split[1]
				}
				query = query.Where(fieldName+" = ?", x.PathParams.Get(fieldName))
				continue
			}
			value, ok := params[fieldName]
			if !ok {
				continue
			}
			valueStr := value.(string)
			fieldType := field.Type
			if fieldType.Kind() == reflect.Ptr {
				fieldType = fieldType.Elem()
			}
			// 如果是字符串类型，使用LIKE进行模糊查询
			if fieldType.Kind() == reflect.String {
				query = query.Where(fieldName+" LIKE ?", "%"+valueStr+"%")
			} else {
				// 其他类型需要转换类型后进行精确匹配
				switch fieldType.Kind() {
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
					if intVal, err := strconv.ParseInt(valueStr, 10, 64); err == nil {
						query = query.Where(fieldName+" = ?", intVal)
					}
				case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					if uintVal, err := strconv.ParseUint(valueStr, 10, 64); err == nil {
						query = query.Where(fieldName+" = ?", uintVal)
					}
				case reflect.Float32, reflect.Float64:
					if floatVal, err := strconv.ParseFloat(valueStr, 64); err == nil {
						query = query.Where(fieldName+" = ?", floatVal)
					}
				case reflect.Bool:
					if boolVal, err := strconv.ParseBool(valueStr); err == nil {
						query = query.Where(fieldName+" = ?", boolVal)
					}
				default:
					// 默认作为字符串处理
					query = query.Where(fieldName+" = ?", valueStr)
				}
			}
		}

		// 计算总数
		var total int64
		query.Count(&total)

		// 查询数据
		if err := query.Debug().Offset(offset).Limit(pageSize).Find(sliceValue).Error; err != nil {
			return nil, err
		}

		totalPages := int((total + int64(pageSize) - 1) / int64(pageSize))

		response := &PaginatedResponse{
			Items:      sliceValue,
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
		}
		return response, nil
	}
}

// Delete 删除资源
func Delete(db func() *gorm.DB, model any) vigo.FuncX2AnyErr {
	modelType := reflect.TypeOf(model)
	if modelType.Kind() == reflect.Ptr {
		modelType = modelType.Elem()
	}

	return func(x *vigo.X) (any, error) {
		id := x.PathParams.Get("id")
		if id == "" {
			return nil, vigo.ErrArgMissing
		}

		modelValue := reflect.New(modelType).Interface()

		if err := db().Delete(modelValue, "id = ?", id).Error; err != nil {
			if err == gorm.ErrRecordNotFound {
				return nil, vigo.ErrNotFound
			}
			return nil, err
		}
		return "ok", nil
	}
}
