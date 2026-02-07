package config

import (
	"log"

	"github.com/spf13/viper"
)

type Config struct {
	Service ServiceConfig `mapstructure:"service"`
	Consul  ConsulConfig  `mapstructure:"consul"`
	Mysql   MysqlConfig   `mapstructure:"mysql"`
	Redis   RedisConfig   `mapstructure:"redis"`
}

type ServiceConfig struct {
	Name string `mapstructure:"name"`
	Port int    `mapstructure:"port"`
}

type ConsulConfig struct {
	Address string `mapstructure:"address"`
}

type MysqlConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	DbName   string `mapstructure:"dbname"`
}

type RedisConfig struct {
	Address  string `mapstructure:"address"`
	Password string `mapstructure:"password"`
	Db       int    `mapstructure:"db"`
}

// LoadConfig 读取配置文件
func LoadConfig(path string) (*Config, error) {
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var config Config
	if err := viper.Unmarshal(&config); err != nil {
		return nil, err
	}

	log.Printf("Config loaded successfully from %s", path)
	return &config, nil
}
