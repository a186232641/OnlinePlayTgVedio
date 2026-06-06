package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
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

	// SyncInterval is how often the background scheduler re-runs an incremental
	// TG sync for every already-synced channel. 0 disables auto-sync.
	SyncInterval time.Duration

	// DCOverrides override Telegram DC addresses (gotd's built-in IPs go stale
	// when Telegram rotates them). Parsed from TG_DC_OVERRIDES, applied on top
	// of dcs.Prod() so the given IP is tried first for that DC.
	DCOverrides []DCOverride
}

// DCOverride pins a Telegram DC id to a specific IPv4/IPv6 address and port.
type DCOverride struct {
	ID   int
	IP   string
	Port int
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

	si, err := parseSyncInterval(env("SYNC_INTERVAL", "30m"))
	if err != nil {
		return nil, fmt.Errorf("SYNC_INTERVAL: %w", err)
	}
	c.SyncInterval = si

	ov, err := parseDCOverrides(env("TG_DC_OVERRIDES", ""))
	if err != nil {
		return nil, fmt.Errorf("TG_DC_OVERRIDES: %w", err)
	}
	c.DCOverrides = ov

	return c, nil
}

// parseDCOverrides parses "5=91.108.56.100,4=149.154.167.91:443" into overrides.
// Each entry is "<dc-id>=<ip>[:<port>]"; the port defaults to 443.
func parseDCOverrides(s string) ([]DCOverride, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []DCOverride
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.IndexByte(part, '=')
		if eq < 0 {
			return nil, fmt.Errorf("bad entry %q (want id=ip[:port])", part)
		}
		id, err := strconv.Atoi(strings.TrimSpace(part[:eq]))
		if err != nil {
			return nil, fmt.Errorf("bad dc id in %q: %w", part, err)
		}
		addr := strings.TrimSpace(part[eq+1:])
		port := 443
		// Split host:port only on the last colon so IPv6 literals survive; an
		// IPv6 address must be bracketed if a port is appended ([::1]:443).
		if h, p, ok := splitHostPort(addr); ok {
			addr = h
			if p != "" {
				port, err = strconv.Atoi(p)
				if err != nil {
					return nil, fmt.Errorf("bad port in %q: %w", part, err)
				}
			}
		}
		if addr == "" {
			return nil, fmt.Errorf("empty ip in %q", part)
		}
		out = append(out, DCOverride{ID: id, IP: addr, Port: port})
	}
	return out, nil
}

// splitHostPort splits "ip:port" / "[ipv6]:port" / "ip" without erroring on a
// bare address. ok is false when there's no port component to split off.
func splitHostPort(addr string) (host, port string, ok bool) {
	if strings.HasPrefix(addr, "[") {
		if end := strings.IndexByte(addr, ']'); end >= 0 {
			host = addr[1:end]
			rest := addr[end+1:]
			if strings.HasPrefix(rest, ":") {
				return host, rest[1:], true
			}
			return host, "", true
		}
		return addr, "", false
	}
	// Bare IPv6 (multiple colons) without brackets → treat whole thing as host.
	if strings.Count(addr, ":") != 1 {
		return addr, "", false
	}
	i := strings.IndexByte(addr, ':')
	return addr[:i], addr[i+1:], true
}

// parseSyncInterval accepts a Go duration ("30m", "1h", "90s"). Empty, "0", or
// "off" disable the scheduler.
func parseSyncInterval(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" || strings.EqualFold(s, "off") {
		return 0, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, errors.New("must be non-negative")
	}
	return d, nil
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
