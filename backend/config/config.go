package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const DefaultTestURL = "http://www.google.com"

const legacyTestURL = "https://www.msftconnecttest.com/connecttest.txt"

type KeyRule struct {
	Match      string `json:"match"`
	Path       string `json:"path"`
	User       string `json:"user,omitempty"`
	Passphrase string `json:"passphrase,omitempty"`
}

// Config never crosses the Wails bridge. Only Summary is exposed to the UI.
type Config struct {
	EnvPath, BaseDir, Ranges, User, KeyPath, KeyPassphrase, KnownHosts string
	ProxyHost, ProxyAuth, ProxyUser, ProxyPassword, TestURL            string
	SSHPort, ProxyPort, LocalPort, Concurrency                         int
	SSHInterval, InternetInterval, ConnectTimeout, CommandTimeout      time.Duration
	AutoScan                                                           bool
	Rules                                                              []KeyRule
	Pools                                                              []Pool
	IPs                                                                []string
}

type Summary struct {
	EnvPath             string `json:"envPath"`
	Ranges              string `json:"ranges"`
	User                string `json:"user"`
	SSHPort             int    `json:"sshPort"`
	KeyPath             string `json:"keyPath"`
	KeyRules            int    `json:"keyRules"`
	KnownHosts          string `json:"knownHosts"`
	ProxyHost           string `json:"proxyHost"`
	ProxyPort           int    `json:"proxyPort"`
	ProxyAuth           string `json:"proxyAuth"`
	ProxyCredentialsSet bool   `json:"proxyCredentialsSet"`
	LocalPort           int    `json:"localPort"`
	TestURL             string `json:"testUrl"`
	SSHInterval         int    `json:"sshInterval"`
	InternetInterval    int    `json:"internetInterval"`
	Concurrency         int    `json:"concurrency"`
}

func (c *Config) Summary() Summary {
	return Summary{c.EnvPath, c.Ranges, c.User, c.SSHPort, c.KeyPath, len(c.Rules), c.KnownHosts, c.ProxyHost, c.ProxyPort, c.ProxyAuth, c.ProxyUser != "" && c.ProxyPassword != "", c.LocalPort, c.TestURL, int(c.SSHInterval.Seconds()), int(c.InternetInterval.Seconds()), c.Concurrency}
}

func ResolveEnv() string {
	if path := os.Getenv("SCANSSH_ENV"); path != "" {
		absolute, _ := filepath.Abs(path)
		return absolute
	}
	if executable, err := os.Executable(); err == nil {
		path := filepath.Join(filepath.Dir(executable), ".env")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	path, _ := filepath.Abs(".env")
	return path
}

func Load(path string) (*Config, error) {
	values, err := godotenv.Read(path)
	if err != nil {
		return nil, fmt.Errorf("не удалось прочитать .env: %s", path)
	}
	get := func(name, fallback string) string {
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		if value, ok := values[name]; ok {
			return value
		}
		return fallback
	}
	absolute, _ := filepath.Abs(path)
	c := &Config{EnvPath: absolute, BaseDir: filepath.Dir(absolute)}
	c.Ranges = get("SCAN_RANGES", "10.0.113.1-30/24,10.0.113.100-130/24,10.32.237.1-30/24")
	c.Pools, c.IPs, err = Expand(c.Ranges)
	if err != nil {
		return nil, err
	}
	c.User = get("SSH_USER", "admin")
	if strings.TrimSpace(c.User) == "" || strings.ContainsAny(c.User, "\r\n\x00") {
		return nil, fmt.Errorf("проверьте SSH_USER")
	}
	c.KeyPath = c.path(get("SSH_KEY_PATH", ""))
	c.KeyPassphrase = get("SSH_KEY_PASSPHRASE", "")
	c.KnownHosts = c.path(get("SSH_KNOWN_HOSTS", "data/known_hosts"))
	if c.KnownHosts == "" {
		return nil, fmt.Errorf("укажите SSH_KNOWN_HOSTS")
	}
	if err = json.Unmarshal([]byte(get("SSH_KEY_RULES", "[]")), &c.Rules); err != nil {
		return nil, fmt.Errorf("SSH_KEY_RULES: ожидается JSON-массив правил")
	}
	if len(c.Rules) > 100 {
		return nil, fmt.Errorf("не более 100 правил ключей")
	}
	for i := range c.Rules {
		rule := &c.Rules[i]
		if _, _, err = Expand(rule.Match); err != nil || rule.Path == "" {
			return nil, fmt.Errorf("SSH_KEY_RULES: некорректное правило %d", i+1)
		}
		rule.Path = c.path(rule.Path)
	}
	c.ProxyHost = get("PROXY_HOST", "10.0.0.1")
	if c.ProxyHost == "" || strings.ContainsAny(c.ProxyHost, "\r\n\x00 /\\\"'@:") {
		return nil, fmt.Errorf("PROXY_HOST: укажите IPv4 или имя без протокола и порта")
	}
	c.ProxyAuth = get("PROXY_AUTH", "basic")
	if c.ProxyAuth != "basic" && c.ProxyAuth != "none" {
		return nil, fmt.Errorf("PROXY_AUTH: поддерживаются basic и none")
	}
	c.ProxyUser, c.ProxyPassword = get("PROXY_USER", ""), get("PROXY_PASSWORD", "")
	if strings.ContainsAny(c.ProxyUser, ":\r\n\x00") || strings.ContainsAny(c.ProxyPassword, "\r\n\x00") {
		return nil, fmt.Errorf("недопустимые символы в профиле прокси")
	}
	c.TestURL = get("TEST_URL", DefaultTestURL)
	// Existing installations keep their .env when the executable is replaced.
	// Migrate only the old built-in probe; preserve custom URLs and credentials.
	if c.TestURL == legacyTestURL {
		c.TestURL = DefaultTestURL
	}
	u, err := url.Parse(c.TestURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(c.TestURL, "\r\n\x00") {
		return nil, fmt.Errorf("TEST_URL: укажите HTTP или HTTPS URL без учётных данных")
	}
	for _, p := range []struct {
		name, fallback string
		target         *int
	}{{"SSH_PORT", "22", &c.SSHPort}, {"PROXY_PORT", "3128", &c.ProxyPort}, {"LOCAL_PORT", "3128", &c.LocalPort}} {
		*p.target, err = Port(get(p.name, p.fallback))
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
	}
	for _, p := range []struct {
		name, fallback string
		target         *time.Duration
	}{{"SSH_INTERVAL_SECONDS", "10", &c.SSHInterval}, {"INTERNET_INTERVAL_SECONDS", "30", &c.InternetInterval}, {"CONNECT_TIMEOUT_SECONDS", "4", &c.ConnectTimeout}, {"COMMAND_TIMEOUT_SECONDS", "10", &c.CommandTimeout}} {
		n, e := strconv.Atoi(get(p.name, p.fallback))
		if e != nil || n < 1 || n > 3600 {
			return nil, fmt.Errorf("%s: число секунд от 1 до 3600", p.name)
		}
		*p.target = time.Duration(n) * time.Second
	}
	c.Concurrency, err = strconv.Atoi(get("SCAN_CONCURRENCY", "12"))
	if err != nil || c.Concurrency < 1 || c.Concurrency > 64 {
		return nil, fmt.Errorf("SCAN_CONCURRENCY: число от 1 до 64")
	}
	c.AutoScan, err = strconv.ParseBool(get("AUTO_SCAN", "true"))
	if err != nil {
		return nil, fmt.Errorf("AUTO_SCAN: true или false")
	}
	return c, nil
}

func (c *Config) path(path string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, "~\\") {
		if home, e := os.UserHomeDir(); e == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(c.BaseDir, path)
}

func (c *Config) Identity(ip string) (path, passphrase, user string) {
	path, passphrase, user = c.KeyPath, c.KeyPassphrase, c.User
	for _, rule := range c.Rules {
		if Match(rule.Match, ip) {
			path = rule.Path
			if rule.Passphrase != "" {
				passphrase = rule.Passphrase
			}
			if rule.User != "" {
				user = rule.User
			}
			break
		}
	}
	return
}

func (c *Config) Address(ip string) string { return net.JoinHostPort(ip, strconv.Itoa(c.SSHPort)) }
