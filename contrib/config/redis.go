//
// redis.go
// Copyright (C) 2026 veypi <i@veypi.com>
//
// Distributed under terms of the MIT license.
//

package config

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type prefixHook struct {
	prefix string
}

func (h *prefixHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	h.addPrefixToCmd(cmd)
	return ctx, nil
}

func (h *prefixHook) AfterProcess(ctx context.Context, cmd redis.Cmder) error {
	return nil
}

func (h *prefixHook) BeforeProcessPipeline(ctx context.Context, cmds []redis.Cmder) (context.Context, error) {
	for _, cmd := range cmds {
		h.addPrefixToCmd(cmd)
	}
	return ctx, nil
}

func (h *prefixHook) AfterProcessPipeline(ctx context.Context, cmds []redis.Cmder) error {
	return nil
}

func (h *prefixHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		return next(ctx, network, addr)
	}
}

func (h *prefixHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		return next(ctx, cmd)
	}
}

func (h *prefixHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		return next(ctx, cmds)
	}
}

func (h *prefixHook) addPrefixToCmd(cmd redis.Cmder) {
	args := cmd.Args()
	if len(args) < 2 {
		return
	}

	cmdName := strings.ToUpper(args[0].(string))

	switch cmdName {
	case "MGET", "MSET", "MSETNX", "DEL", "UNLINK", "EXISTS", "TOUCH":
		for i := 1; i < len(args); i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "BLPOP", "BRPOP", "BRPOPLPUSH", "BLMOVE":
		for i := 1; i < len(args)-1; i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "BITOP":
		for i := 2; i < len(args); i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "RENAME", "RENAMENX":
		// RENAME key newkey - arg1=key, arg2=newkey
		for i := 1; i < len(args) && i <= 2; i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "RPOPLPUSH", "LMOVE", "SMOVE":
		// SMOVE source destination member - arg1=source, arg2=dest (both are keys)
		for i := 1; i < len(args) && i <= 2; i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "SDIFFSTORE", "SINTERSTORE", "SUNIONSTORE":
		// SDIFFSTORE destination key [key ...] - arg1=dest, arg2+ =source keys
		for i := 1; i < len(args); i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "ZDIFFSTORE", "ZINTERSTORE", "ZUNIONSTORE":
		// ZINTERSTORE destination numkeys key [key ...] - arg1=dest, arg2=numkeys, arg3+ =source keys
		args[1] = h.prefixKey(args[1])
		for i := 3; i < len(args); i++ {
			args[i] = h.prefixKey(args[i])
		}
	case "LMPOP", "BLMPOP", "ZMPOP", "BZMPOP":
		// LMPOP numkeys key [key ...] timeout - arg1=numkeys, arg2+ =keys (except last is timeout)
		// numkeys determines how many keys follow
		if len(args) >= 2 {
			numkeys, ok := args[1].(int)
			if !ok {
				// try string conversion
				if s, ok := args[1].(string); ok {
					fmt.Sscanf(s, "%d", &numkeys)
				}
			}
			for i := 2; i < len(args)-1 && i < 2+numkeys; i++ {
				args[i] = h.prefixKey(args[i])
			}
		}
	default:
		args[1] = h.prefixKey(args[1])
	}
}

func (h *prefixHook) prefixKey(key any) any {
	s, ok := key.(string)
	if !ok || s == "" || strings.HasPrefix(s, h.prefix) {
		return key
	}
	return h.prefix + s
}

type Redis struct {
	Addr     string `json:"addr" desc:"support 'memory' and redis addr."`
	Password string `json:"password"`
	DB       int    `json:"db"`
	Prefix   string `json:"prefix"`

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
		if r.Prefix != "" {
			r.client.AddHook(&prefixHook{prefix: r.Prefix})
		}
	})
	return r.client
}
