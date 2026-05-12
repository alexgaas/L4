package config

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	StatusPort int      `yaml:"status_port"`
	ZkServers  []string `yaml:"zk_servers"`
	Servers    []ConfigServer
}

type ConfigServer struct {
	Port       int
	Duplicator []ConfigDuplicator
}

type ConfigDuplicator struct {
	Balancer *ConfigBalancer
	Backend  *ConfigBackend
}

type ConfigBackend struct {
	Addr            string
	CheckURL        string `yaml:"check_url"`
	PreserveSrcAddr bool   `yaml:"preserve_src_addr"`
	ResolveOnlyIPV6 bool   `yaml:"resolve_only_ipv6"`
	FakeSrcIPv4Addr string `yaml:"fake_src_ipv4_addr"`

	Weight float64
	UUID   string
}

type ConfigBalancer struct {
	Backends  []ConfigBackend
	Algorithm string
	Ipfix     bool
}

func ReadConfig(configName string) (*Config, error) {
	configData, err := ioutil.ReadFile(configName)
	if err != nil {
		return nil, err
	}

	config := Config{}

	err = yaml.UnmarshalStrict(configData, &config)
	if err != nil {
		return nil, err
	}

	return &config, nil
}
