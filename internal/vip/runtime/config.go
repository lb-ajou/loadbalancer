package runtime

type Config struct {
	Interface         string
	Address           string
	GARPCount         int
	GARPInterval      string
	AcquireDelay      string
	ReleaseOnShutdown bool
}

func (c Config) Active() bool {
	return c.Address != ""
}
