package crud

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/utils"
	"gorm.io/gorm"
)

// Register registers CRUD routes for a model.
// model: A pointer to the model struct (e.g. &User{}).
// The database instance must be injected into the context with key "db".
func Register[T any](r vigo.Router, model T) {
	// Get struct name via reflection
	t := reflect.TypeOf(model)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		panic("crud: model must be a struct or pointer to struct")
	}

	structName := t.Name()
	idParam := utils.CamelToSnake(structName) + "_id"

	c := &controller[T]{
		idParam: idParam,
	}

	// GET / - List
	r.Get("/", "List resources", c.List)
	// POST / - Create
	r.Post("/", "Create resource", c.Create)
	// GET /{id} - Get one
	r.Get(fmt.Sprintf("/{%s}", idParam), "Get resource", c.Get)
	// DELETE /{id} - Delete one
	r.Delete(fmt.Sprintf("/{%s}", idParam), "Delete resource", c.Delete)
	// PATCH /{id} - Update one (partial)
	r.Patch(fmt.Sprintf("/{%s}", idParam), "Update resource", c.Update)
}

type controller[T any] struct {
	idParam string
}

func (c *controller[T]) getDB(x *vigo.X) (*gorm.DB, error) {
	db, ok := x.Get("db").(func() *gorm.DB)
	if !ok {
		return nil, vigo.ErrInternalServer.WithMessage("invalid database instance in context")
	}
	return db(), nil
}

type ListReq struct {
	Page int `src:"query" default:"1" json:"page"`
	Size int `src:"query" default:"20" json:"size"`
}

type ListResponse[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

func (c *controller[T]) List(x *vigo.X, req *ListReq) (*ListResponse[T], error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	var items []T
	var total int64
	offset := (req.Page - 1) * req.Size

	model := new(T)
	tx := db.Model(model)

	if err := tx.Count(&total).Error; err != nil {
		return nil, vigo.ErrInternalServer.WithError(err)
	}

	if err := tx.Offset(offset).Limit(req.Size).Find(&items).Error; err != nil {
		return nil, vigo.ErrInternalServer.WithError(err)
	}

	return &ListResponse[T]{
		Items: items,
		Total: total,
		Page:  req.Page,
		Size:  req.Size,
	}, nil
}

func (c *controller[T]) Get(x *vigo.X) (*T, error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	id := x.PathParams.Get(c.idParam)
	if id == "" {
		return nil, vigo.ErrArgInvalid.WithArgs(c.idParam)
	}

	var item T
	if err := db.Where("id = ?", id).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, vigo.ErrNotFound
		}
		return nil, vigo.ErrInternalServer.WithError(err)
	}
	return &item, nil
}

func (c *controller[T]) Create(x *vigo.X, req *T) (*T, error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	if err := db.Create(req).Error; err != nil {
		return nil, vigo.ErrInternalServer.WithError(err)
	}
	return req, nil
}

func (c *controller[T]) Delete(x *vigo.X) (any, error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	id := x.PathParams.Get(c.idParam)
	if id == "" {
		return nil, vigo.ErrArgInvalid.WithArgs(c.idParam)
	}

	model := new(T)
	if err := db.Where("id = ?", id).Delete(model).Error; err != nil {
		return nil, vigo.ErrInternalServer.WithError(err)
	}
	return "success", nil
}

func (c *controller[T]) Update(x *vigo.X) (*T, error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	id := x.PathParams.Get(c.idParam)
	if id == "" {
		return nil, vigo.ErrArgInvalid.WithArgs(c.idParam)
	}

	// Parse body manually to map to support partial updates
	var data map[string]any
	if err := json.NewDecoder(x.Request.Body).Decode(&data); err != nil {
		if err != io.EOF {
			return nil, vigo.NewError("Invalid JSON").WithCode(400)
		}
	}

	// Remove ID from update data to prevent changing it
	delete(data, "id")
	delete(data, "ID")
	// Also remove the param name ID if present
	delete(data, c.idParam)

	if len(data) > 0 {
		model := new(T)
		if err := db.Model(model).Where("id = ?", id).Updates(data).Error; err != nil {
			return nil, vigo.ErrInternalServer.WithError(err)
		}
	}

	// Retrieve updated item
	var updatedItem T
	if err := db.Where("id = ?", id).First(&updatedItem).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, vigo.ErrNotFound
		}
		return nil, vigo.ErrInternalServer.WithError(err)
	}

	return &updatedItem, nil
}
