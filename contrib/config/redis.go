//
// redis.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package config

import (
	"sync"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type Redis struct {
	Addr     string `json:"addr" desc:"support 'memory' and redis addr."`
	Password string `json:"password"`
	DB       int    `json:"db"`

	once   sync.Once
	client *redis.Client
}

func (r *Redis) Client() *redis.Client {
	r.once.Do(func() {
		if r.Addr == "memory" || r.Addr == "" {
			mr, err := miniredis.Run()
			if err != nil {
				panic(err)
			}
			r.client = redis.NewClient(&redis.Options{
				Addr: mr.Addr(),
			})
		} else {
			r.client = redis.NewClient(&redis.Options{
				Addr:     r.Addr,
				Password: r.Password,
				DB:       r.DB,
			})
		}
	})
	return r.client
}
