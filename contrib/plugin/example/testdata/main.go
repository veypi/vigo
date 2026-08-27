package main

import (
	"fmt"

	"github.com/veypi/vigo"
)

var Router = vigo.NewRouter()

func init() {
	Router.Get("/hello", func(x *vigo.X) error {
		return x.JSON("Hello from Plugin!")
	})

	Router.Get("/echo/{msg}", func(x *vigo.X) error {
		return x.JSON(fmt.Sprintf("Echo: %s", x.PathParams.Get("msg")))
	})

	// Test handler returning struct (Onion model)
	Router.Get("/user", getUser)
}

type User struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

type UserReq struct{}

func getUser(x *vigo.X, _ *UserReq) (*User, error) {
	return &User{Name: "Vigo", Age: 1}, nil
}

func Init() error {
	return nil
}
