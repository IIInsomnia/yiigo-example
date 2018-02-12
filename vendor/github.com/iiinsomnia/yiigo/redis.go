package yiigo

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/garyburd/redigo/redis"
	toml "github.com/pelletier/go-toml"
)

type redisConf struct {
	Name          string `toml:"name"`
	Host          string `toml:"host"`
	Port          int    `toml:"port"`
	Password      string `toml:"password"`
	Database      int    `toml:"database"`
	ConnTimeout   int    `toml:"connTimeout"`
	ReadTimeout   int    `toml:"readTimeout"`
	WriteTimeout  int    `toml:"writeTimeout"`
	MaxIdleConn   int    `toml:"maxIdleConn"`
	MaxActiveConn int    `toml:"maxActiveConn"`
	IdleTimeout   int    `toml:"idleTimeout"`
	TestOnBorrow  int    `toml:"testOnBorrow"`
	PoolWait      bool   `toml:"poolWait"`
}

var (
	// Redis default connection pool
	Redis    *redis.Pool
	redisMap sync.Map
)

func initRedis() error {
	var err error

	result := Env.Get("redis")

	switch node := result.(type) {
	case *toml.Tree:
		conf := &redisConf{}
		err = node.Unmarshal(conf)

		if err != nil {
			break
		}

		err = initSingleRedis(conf)
	case []*toml.Tree:
		conf := make([]*redisConf, 0, len(node))

		for _, v := range node {
			c := &redisConf{}
			err = v.Unmarshal(c)

			if err != nil {
				break
			}

			conf = append(conf, c)
		}

		err = initMultiRedis(conf)
	default:
		return errors.New("redis error config")
	}

	if err != nil {
		return fmt.Errorf("redis error: %s", err.Error())
	}

	return nil
}

func initSingleRedis(conf *redisConf) error {
	var err error

	Redis, err = redisDial(conf)

	return err
}

func initMultiRedis(conf []*redisConf) error {
	for _, v := range conf {
		p, err := redisDial(v)

		if err != nil {
			return err
		}

		redisMap.Store(v.Name, p)
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
				redis.DialConnectTimeout(time.Duration(conf.ConnTimeout) * time.Millisecond),
				redis.DialReadTimeout(time.Duration(conf.ReadTimeout) * time.Millisecond),
				redis.DialWriteTimeout(time.Duration(conf.WriteTimeout) * time.Millisecond),
			}

			conn, err := redis.Dial("tcp", dsn, dialOptions...)

			return conn, err
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			if conf.TestOnBorrow == 0 || time.Since(t) < time.Duration(conf.TestOnBorrow)*time.Millisecond {
				return nil
			}
			_, err := c.Do("PING")

			return err
		},
		MaxIdle:     conf.MaxIdleConn,
		MaxActive:   conf.MaxActiveConn,
		IdleTimeout: time.Duration(conf.IdleTimeout) * time.Millisecond,
		Wait:        conf.PoolWait,
	}

	conn := pool.Get()
	defer conn.Close()

	_, err := conn.Do("PING")

	if err != nil {
		return nil, err
	}

	return pool, nil
}

// RedisPool get redis connection pool
func RedisPool(conn ...string) (*redis.Pool, error) {
	schema := "default"

	if len(conn) > 0 {
		schema = conn[0]
	}

	v, ok := redisMap.Load(schema)

	if !ok {
		return nil, fmt.Errorf("redis %s is not connected", schema)
	}

	return v.(*redis.Pool), nil
}

// ScanJSON scans json string to the struct or struct slice pointed to by dest
func ScanJSON(reply interface{}, dest interface{}) error {
	v := reflect.Indirect(reflect.ValueOf(dest))

	var err error

	switch v.Kind() {
	case reflect.Struct:
		err = scanJSONObj(reply, dest)
	case reflect.Slice:
		err = scanJSONSlice(reply, dest)
	}

	return err
}

func scanJSONObj(reply interface{}, dest interface{}) error {
	bytes, err := redis.Bytes(reply, nil)

	if err != nil {
		return err
	}

	err = json.Unmarshal(bytes, dest)

	if err != nil {
		return err
	}

	return nil
}

func scanJSONSlice(reply interface{}, dest interface{}) error {
	bytes, err := redis.ByteSlices(reply, nil)

	if err != nil {
		return err
	}

	if len(bytes) == 0 {
		return nil
	}

	v := reflect.Indirect(reflect.ValueOf(dest))

	if v.Kind() != reflect.Slice {
		return errors.New("the dest must be a slice")
	}

	t := v.Type()
	v.Set(reflect.MakeSlice(t, 0, 0))

	for _, b := range bytes {
		elem := reflect.New(t.Elem()).Elem()
		err := json.Unmarshal(b, elem.Addr().Interface())

		if err != nil {
			return err
		}

		v.Set(reflect.Append(v, elem))
	}

	return nil
}
