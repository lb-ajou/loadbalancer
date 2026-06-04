//go:build !linux

package vip

import (
	"context"
	"fmt"
)

type NetlinkManager struct{}

func NewNetlinkManager(_, _ string) (*NetlinkManager, error) {
	return nil, fmt.Errorf("vip netlink manager requires linux")
}

func (m *NetlinkManager) Add(context.Context) error {
	return fmt.Errorf("vip netlink manager requires linux")
}

func (m *NetlinkManager) Remove(context.Context) error {
	return fmt.Errorf("vip netlink manager requires linux")
}
