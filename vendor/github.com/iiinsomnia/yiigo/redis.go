package yiigo

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/gomodule/redigo/redis"
	toml "github.com/pelletier/go-toml"
)

type redisConf struct {
	Name            string `toml:"name"`
	Host            string `toml:"host"`
	Port            int    `toml:"port"`
	Password        string `toml:"password"`
	Database        int    `toml:"database"`
	ConnTimeout     int    `toml:"connTimeout"`
	ReadTimeout     int    `toml:"readTimeout"`
	WriteTimeout    int    `toml:"writeTimeout"`
	MaxIdleConn     int    `toml:"maxIdleConn"`
	MaxActiveConn   int    `toml:"maxActiveConn"`
	MaxConnLifetime int    `toml:"maxConnLifetime"`
	IdleTimeout     int    `toml:"idleTimeout"`
	TestOnBorrow    int    `toml:"testOnBorrow"`
	PoolWait        bool   `toml:"poolWait"`
}

var (
	// Redis default redis connection pool
	Redis    *redis.Pool
	redisMap sync.Map
)

func initRedis() error {
	result := Env.Get("redis")

	if result == nil {
		color.Blue("[yiigo] no redis configured")

		return nil
	}

	switch node := result.(type) {
	case *toml.Tree:
		conf := &redisConf{}
		err := node.Unmarshal(conf)

		if err != nil {
			return err
		}

		err = initSingleRedis(conf)

		if err != nil {
			return err
		}
	case []*toml.Tree:
		conf := make([]*redisConf, 0, len(node))

		for _, v := range node {
			c := &redisConf{}
			err := v.Unmarshal(c)

			if err != nil {
				return err
			}

			conf = append(conf, c)
		}

		err := initMultiRedis(conf)

		if err != nil {
			return err
		}
	default:
		return errors.New("yiigo: invalid redis config")
	}

	return nil
}

func initSingleRedis(conf *redisConf) error {
	var err error

	Redis, err = redisDial(conf)

	if err != nil {
		return fmt.Errorf("yiigo: redis.default connect error: %s", err.Error())
	}

	redisMap.Store("default", Redis)

	color.Green("[yiigo] redis.default connect success")

	return nil
}

func initMultiRedis(conf []*redisConf) error {
	for _, v := range conf {
		p, err := redisDial(v)

		if err != nil {
			return fmt.Errorf("yiigo: redis.%s connect error: %s", v.Name, err.Error())
		}

		redisMap.Store(v.Name, p)

		color.Green("[yiigo] redis.%s connect success", v.Name)
	}

	if v, ok := redisMap.Load("default"); ok {
		Redis = v.(*redis.Pool)
	}

	return nil
}

func redisDial(conf *redisConf) (*redis.Pool, error) {
	pool := &redis.Pool{
		Dial: func() (redis.Conn, error) {
			dsn := fmt.Sprintf("%s:%d", conf.Host, conf.Port)

			dialOptions := []redis.DialOption{
				redis.DialPassword(conf.Password),
				redis.DialDatabase(conf.Database),
				redis.DialConnectTimeout(time.Duration(conf.ConnTimeout) * time.Second),
				redis.DialReadTimeout(time.Duration(conf.ReadTimeout) * time.Second),
				redis.DialWriteTimeout(time.Duration(conf.WriteTimeout) * time.Second),
			}

			conn, err := redis.Dial("tcp", dsn, dialOptions...)

			return conn, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if conf.TestOnBorrow == 0 || time.Since(t) < time.Duration(conf.TestOnBorrow)*time.Second {
				return nil
			}

			_, err := c.Do("PING")

			return err
		},
		MaxIdle:         conf.MaxIdleConn,
		MaxActive:       conf.MaxActiveConn,
		IdleTimeout:     time.Duration(conf.IdleTimeout) * time.Second,
		MaxConnLifetime: time.Duration(conf.MaxConnLifetime) * time.Second,
		Wait:            conf.PoolWait,
	}

	conn := pool.Get()
	defer conn.Close()

	_, err := conn.Do("PING")

	if err != nil {
		return nil, err
	}

	return pool, nil
}

// RedisPool returns a redis connection pool.
func RedisPool(conn ...string) (*redis.Pool, error) {
	schema := "default"

	if len(conn) > 0 {
		schema = conn[0]
	}

	v, ok := redisMap.Load(schema)

	if !ok {
		return nil, fmt.Errorf("yiigo: redis.%s is not connected", schema)
	}

	return v.(*redis.Pool), nil
}
