//go:build linux

package vip

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"time"

	"github.com/mdlayher/arp"
	"github.com/mdlayher/ethernet"
)

type ARPAnnouncer struct {
	iface    *net.Interface
	ip       netip.Addr
	count    int
	interval time.Duration
}

func NewARPAnnouncer(interfaceName, cidr string, count int, interval time.Duration) (*ARPAnnouncer, error) {
	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("find vip interface: %w", err)
	}
	ip, err := parseARPAddr(cidr)
	if err != nil {
		return nil, err
	}
	return &ARPAnnouncer{iface: iface, ip: ip, count: count, interval: interval}, nil
}

func (a *ARPAnnouncer) Announce(ctx context.Context) error {
	client, err := arp.Dial(a.iface)
	if err != nil {
		return fmt.Errorf("dial arp client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()
	packet, err := a.packet(client.HardwareAddr())
	if err != nil {
		return err
	}
	return a.writeAnnouncements(ctx, client, packet)
}

func parseARPAddr(cidr string) (netip.Addr, error) {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("parse vip address: %w", err)
	}
	if !prefix.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("vip address must be IPv4")
	}
	return prefix.Addr(), nil
}

func (a *ARPAnnouncer) packet(hw net.HardwareAddr) (*arp.Packet, error) {
	packet, err := arp.NewPacket(arp.OperationReply, hw, a.ip, ethernet.Broadcast, a.ip)
	if err != nil {
		return nil, fmt.Errorf("build garp packet: %w", err)
	}
	return packet, nil
}

func (a *ARPAnnouncer) writeAnnouncements(ctx context.Context, client *arp.Client, packet *arp.Packet) error {
	for i := 0; i < a.count; i++ {
		if err := client.WriteTo(packet, ethernet.Broadcast); err != nil {
			return fmt.Errorf("send garp packet: %w", err)
		}
		if !a.waitInterval(ctx, i) {
			return ctx.Err()
		}
	}
	return nil
}

func (a *ARPAnnouncer) waitInterval(ctx context.Context, index int) bool {
	if index == a.count-1 || a.interval <= 0 {
		return true
	}
	timer := time.NewTimer(a.interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
