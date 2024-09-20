package config

type Config struct {
	Manager *ManagerConfig `json:"manager"`
}

func NewConfig() *Config {
	return &Config{
		Manager: newManagerConfig(),
	}
}
