package config

import (
	"errors"
	"fmt"
	"io/ioutil"
	"strings"

	yaml "gopkg.in/yaml.v2"
)

type Config struct {
	ServerConfig  *ServerConfig
	MetricsConfig *MetricsConfig
}

type ServerConfig struct {
	Endpoint  string
	CertPath  string
	KeyPath   string
	SecPolicy string
	SecMode   string
	AuthMode  string
	Username  string
	Password  string
}

type MetricsConfig struct {
	Metrics []Metric `yaml:"metrics"`
}

type Metric struct {
	Name   string            `yaml:"name"`
	Help   string            `yaml:"help"`
	NodeID string            `yaml:"nodeid"`
	Labels map[string]string `yaml:"labels"`
	Type   string            `yaml:"type"`
}

func NewConfig(endpoint, certPath, keyPath, secMode, secPolicy, authMode, username, password, configPath string) (*Config, error) {
	c := &Config{
		ServerConfig: &ServerConfig{
			Endpoint:  endpoint,
			CertPath:  certPath,
			KeyPath:   keyPath,
			SecMode:   secMode,
			SecPolicy: secPolicy,
			AuthMode:  authMode,
			Username:  username,
			Password:  password,
		},
		MetricsConfig: &MetricsConfig{},
	}
	if err := c.LoadMetricsConfig(configPath); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) LoadMetricsConfig(filename string) error {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			return nil
		}
		return err
	}
	if err = (c.MetricsConfig).Unserialize(content); err != nil {
		return err
	}
	if err := c.MetricsConfig.validate(); err != nil {
		return err
	}
	return nil
}

func WriteFile(filename string, content []byte) error {
	if err := ioutil.WriteFile(filename, content, 0644); err != nil {
		return err
	}
	return nil
}

func (mm MetricsConfig) validate() error {
	if len(mm.Metrics) == 0 {
		return errors.New("missing field 'metrics' in top configuration")
	}
	for i, m := range mm.Metrics {
		if m.Name == "" {
			return errors.New("missing field 'name' in 'metrics' configuration of metric " + fmt.Sprint(i))
		}
		if m.Help == "" {
			return errors.New("missing field 'help' in 'metrics' configuration of metric " + fmt.Sprint(i))
		}
		if m.NodeID == "" {
			return errors.New("missing field 'nodeValue' in 'metrics' configuration of metric " + fmt.Sprint(i))
		}
		if m.Type == "" {
			return errors.New("missing field 'type' in 'metrics' configuration of metric " + fmt.Sprint(i))
		}
	}
	return nil
}

func (mm *MetricsConfig) Unserialize(content []byte) error {
	return yaml.Unmarshal(content, mm)
}

func (cfg *MetricsConfig) Serialize() ([]byte, error) {
	return yaml.Marshal(cfg)
}
