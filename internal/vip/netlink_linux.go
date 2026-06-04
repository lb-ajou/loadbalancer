//go:build linux

package vip

import (
	"context"
	"fmt"

	"github.com/vishvananda/netlink"
)

type NetlinkManager struct {
	interfaceName string
	addr          netlink.Addr
}

func NewNetlinkManager(interfaceName, cidr string) (*NetlinkManager, error) {
	addr, err := parseNetlinkAddr(cidr)
	if err != nil {
		return nil, err
	}
	return &NetlinkManager{interfaceName: interfaceName, addr: addr}, nil
}

func (m *NetlinkManager) Add(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link, err := m.link()
	if err != nil {
		return err
	}
	exists, err := m.exists(link)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return netlink.AddrAdd(link, &m.addr)
}

func (m *NetlinkManager) Remove(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	link, err := m.link()
	if err != nil {
		return err
	}
	exists, err := m.exists(link)
	if err != nil || !exists {
		return err
	}
	return netlink.AddrDel(link, &m.addr)
}

func parseNetlinkAddr(cidr string) (netlink.Addr, error) {
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return netlink.Addr{}, fmt.Errorf("parse vip address: %w", err)
	}
	if addr.IP.To4() == nil {
		return netlink.Addr{}, fmt.Errorf("vip address must be IPv4")
	}
	return *addr, nil
}

func (m *NetlinkManager) link() (netlink.Link, error) {
	link, err := netlink.LinkByName(m.interfaceName)
	if err != nil {
		return nil, fmt.Errorf("find vip interface: %w", err)
	}
	return link, nil
}

func (m *NetlinkManager) exists(link netlink.Link) (bool, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return false, fmt.Errorf("list vip addresses: %w", err)
	}
	for _, addr := range addrs {
		if addr.Equal(m.addr) {
			return true, nil
		}
	}
	return false, nil
}
