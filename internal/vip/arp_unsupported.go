//go:build !linux

package vip

import (
	"context"
	"fmt"
	"time"
)

type ARPAnnouncer struct{}

func NewARPAnnouncer(_, _ string, _ int, _ time.Duration) (*ARPAnnouncer, error) {
	return nil, fmt.Errorf("vip arp announcer requires linux")
}

func (a *ARPAnnouncer) Announce(context.Context) error {
	return fmt.Errorf("vip arp announcer requires linux")
}
