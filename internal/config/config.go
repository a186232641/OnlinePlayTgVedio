package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ServerAddr string
	Domain     string

	TgAPIID   int
	TgAPIHash string

	DBDSN string

	JWTSecret []byte
	MasterKey []byte

	CacheDir   string
	CacheCapGB int64
}

func Load() (*Config, error) {
	c := &Config{
		ServerAddr: env("SERVER_ADDR", ":8080"),
		Domain:     env("DOMAIN", "localhost"),
		TgAPIHash:  env("TG_API_HASH", ""),
		DBDSN:      env("DB_DSN", ""),
		CacheDir:   env("CACHE_DIR", "/var/cache/tgvideo"),
	}

	if v := os.Getenv("TG_API_ID"); v != "" {
		id, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("TG_API_ID: %w", err)
		}
		c.TgAPIID = id
	}

	jwtSec := env("JWT_SECRET", "")
	if jwtSec == "" {
		return nil, errors.New("JWT_SECRET is required")
	}
	c.JWTSecret = []byte(jwtSec)

	masterKeyB64 := env("MASTER_KEY", "")
	if masterKeyB64 == "" {
		return nil, errors.New("MASTER_KEY is required (32 bytes, base64)")
	}
	mk, err := decodeKey(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("MASTER_KEY: %w", err)
	}
	if len(mk) != 32 {
		return nil, fmt.Errorf("MASTER_KEY must decode to 32 bytes, got %d", len(mk))
	}
	c.MasterKey = mk

	if c.DBDSN == "" {
		return nil, errors.New("DB_DSN is required")
	}
	if c.TgAPIID == 0 || c.TgAPIHash == "" {
		return nil, errors.New("TG_API_ID and TG_API_HASH are required")
	}

	if v := env("CACHE_CAP_GB", "50"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("CACHE_CAP_GB: %w", err)
		}
		c.CacheCapGB = n
	}

	return c, nil
}

func (c *Config) CacheCapBytes() int64 {
	return c.CacheCapGB * 1024 * 1024 * 1024
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawStdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return nil, errors.New("not valid base64")
}
