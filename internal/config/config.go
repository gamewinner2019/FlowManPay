package config

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

var (
	cfg        *Config
	cfgLoadErr error
	once       sync.Once
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Redis    RedisConfig    `yaml:"redis"`
	JWT      JWTConfig      `yaml:"jwt"`
	System   SystemConfig   `yaml:"system"`
	Telegram TelegramConfig `yaml:"telegram"`
}

type ServerConfig struct {
	Port int    `yaml:"port"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Host         string `yaml:"host"`
	Port         int    `yaml:"port"`
	User         string `yaml:"user"`
	Password     string `yaml:"password"`
	DBName       string `yaml:"dbname"`
	Charset      string `yaml:"charset"`
	MaxIdleConns int    `yaml:"max_idle_conns"`
	MaxOpenConns int    `yaml:"max_open_conns"`
	LogLevel     string `yaml:"log_level"`
}

func (d *DatabaseConfig) DSN() string {
	return d.User + ":" + d.Password + "@tcp(" + d.Host + ":" +
		itoa(d.Port) + ")/" + d.DBName + "?charset=" + d.Charset +
		"&parseTime=True&loc=Local"
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

type RedisConfig struct {
	Host     string `yaml:"host"`
	Port     int    `yaml:"port"`
	Password string `yaml:"password"`
	DB       int    `yaml:"db"`
}

func (r *RedisConfig) Addr() string {
	return r.Host + ":" + itoa(r.Port)
}

type JWTConfig struct {
	Secret              string `yaml:"secret"`
	AccessExpireMinutes int    `yaml:"access_expire_minutes"`
	RefreshExpireMinutes int   `yaml:"refresh_expire_minutes"`
}

type SystemConfig struct {
	TablePrefix        string `yaml:"table_prefix"`
	CaptchaState       bool   `yaml:"captcha_state"`
	SingleLogin        bool   `yaml:"single_login"`
	DefaultPassword    string `yaml:"default_password"`
	Timezone           string `yaml:"timezone"`
	LoginNoCaptchaAuth bool   `yaml:"login_no_captcha_auth"`
}

type TelegramConfig struct {
	BotHost      string `yaml:"bot_host"`
	ForwardsPath string `yaml:"forwards_path"`
	BalanceMsg   string `yaml:"balance_msg"`
}

// Load reads the config file and returns the Config struct.
func Load(path string) (*Config, error) {
	once.Do(func() {
		data, err := os.ReadFile(path)
		if err != nil {
			cfgLoadErr = err
			return
		}
		cfg = &Config{}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			cfgLoadErr = err
			cfg = nil
			return
		}
	})
	return cfg, cfgLoadErr
}

// Get returns the loaded config. Must call Load first.
func Get() *Config {
	return cfg
}
