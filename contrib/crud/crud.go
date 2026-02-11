package crud

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"github.com/veypi/vigo"
	"github.com/veypi/vigo/logv"
	"github.com/veypi/vigo/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// Register registers CRUD routes for a model.
// model: A pointer to the model struct (e.g. &User{}).
// The database instance must be injected into the context with key "db".
func New[T any](model T) *Controller[T] {
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

	var listQueryFields []string
	namingStrategy := schema.NamingStrategy{}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if strings.Contains(strings.ToLower(field.Name), "name") {
			// Use GORM's naming strategy to get the column name
			dbName := namingStrategy.ColumnName("", field.Name)
			listQueryFields = append(listQueryFields, dbName)
		}
	}

	return &Controller[T]{
		name:            structName,
		idParam:         idParam,
		listQueryFields: listQueryFields,
	}
}

type Controller[T any] struct {
	name            string
	idParam         string
	listQueryFields []string
	filter          func(*vigo.X, *gorm.DB) (*gorm.DB, error)
}

// SetIDParam sets the URL parameter name for the ID (default is {struct_name}_id)
func (c *Controller[T]) SetIDParam(name string) *Controller[T] {
	c.idParam = name
	return c
}

// SetListQueryFields sets the database fields to search against when the 'query' parameter is provided in ListReq
func (c *Controller[T]) SetListQueryFields(fields ...string) *Controller[T] {
	c.listQueryFields = fields
	return c
}

// SetFilter sets a custom filter function for all actions
func (c *Controller[T]) SetFilter(filter func(*vigo.X, *gorm.DB) (*gorm.DB, error)) *Controller[T] {
	c.filter = filter
	return c
}

// Register registers CRUD routes.
// actions: Optional list of actions to register ("list", "create", "get", "update", "delete").
// If no actions are provided, all routes are registered.
func (c *Controller[T]) Register(r vigo.Router, actions ...string) *Controller[T] {
	if len(actions) == 0 {
		actions = []string{"list", "create", "get", "update", "delete"}
	}

	for _, action := range actions {
		switch action {
		case "list":
			// GET / - List
			r.Get("/", "List "+c.name, c.list)
		case "create":
			// POST / - Create
			r.Post("/", "Create "+c.name, c.create)
		case "get":
			// GET /{id} - Get one
			r.Get(fmt.Sprintf("/{%s}", c.idParam), "Get "+c.name, c.get)
		case "update":
			// PATCH /{id} - Update one (partial)
			r.Patch(fmt.Sprintf("/{%s}", c.idParam), "Update "+c.name, c.update)
		case "delete":
			// DELETE /{id} - Delete one
			r.Delete(fmt.Sprintf("/{%s}", c.idParam), "Delete "+c.name, c.delete)
		default:
			logv.Warn().Msgf("crud: unknown action %s", action)
		}
	}
	return c
}

func (c *Controller[T]) getDB(x *vigo.X) (*gorm.DB, error) {
	dbFn, ok := x.Get("db").(func() *gorm.DB)
	if !ok {
		return nil, vigo.ErrInternalServer.WithMessage("invalid database instance in context")
	}
	db := dbFn()
	if c.filter != nil {
		var err error
		db, err = c.filter(x, db)
		if err != nil {
			return nil, err
		}
	}
	return db, nil
}

type ListReq struct {
	Page  int    `src:"query" default:"1" json:"page"`
	Size  int    `src:"query" default:"20" json:"size"`
	Sort  string `src:"query" json:"sort"`
	Query string `src:"query" json:"query"`
}

type ListResponse[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int   `json:"page"`
	Size  int   `json:"size"`
}

func (c *Controller[T]) list(x *vigo.X, req *ListReq) (*ListResponse[T], error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	var items []T
	var total int64
	offset := (req.Page - 1) * req.Size

	model := new(T)
	tx := db.Model(model)

	if req.Sort != "" {
		tx = tx.Order(req.Sort)
	}

	if req.Query != "" && len(c.listQueryFields) > 0 {
		var queryTx *gorm.DB
		for i, field := range c.listQueryFields {
			cond := fmt.Sprintf("%s LIKE ?", field)
			val := "%" + req.Query + "%"
			if i == 0 {
				queryTx = db.Where(cond, val)
			} else {
				queryTx = queryTx.Or(cond, val)
			}
		}
		tx = tx.Where(queryTx)
	}

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

func (c *Controller[T]) get(x *vigo.X) (*T, error) {
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

func (c *Controller[T]) create(x *vigo.X, req *T) (*T, error) {
	db, err := c.getDB(x)
	if err != nil {
		return nil, err
	}

	if err := db.Create(req).Error; err != nil {
		return nil, vigo.ErrInternalServer.WithError(err)
	}
	return req, nil
}

func (c *Controller[T]) delete(x *vigo.X) (any, error) {
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

func (c *Controller[T]) update(x *vigo.X) (*T, error) {
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
