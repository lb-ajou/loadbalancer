package config

type VIPConfig struct {
	Interface         string
	Address           string
	GARPCount         int
	GARPInterval      string
	AcquireDelay      string
	ReleaseOnShutdown bool
}

func (c VIPConfig) Active() bool {
	return c.Address != ""
}
