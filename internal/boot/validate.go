package boot

import (
	"errors"
)

func Validate(cfg AppConfig) error {
	if err := validateBase(cfg); err != nil {
		return err
	}
	if err := validateRaft(cfg); err != nil {
		return err
	}
	return nil
}

func validateBase(cfg AppConfig) error {
	if cfg.ProxyListenAddr == "" {
		return errors.New("proxy listen address is required")
	}
	if cfg.DashboardListenAddr == "" {
		return errors.New("dashboard listen address is required")
	}
	return nil
}

func validateRaft(cfg AppConfig) error {
	if cfg.RaftDataDir == "" {
		return errors.New("raft data dir is required")
	}
	return nil
}
