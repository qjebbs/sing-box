package adapter

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/sagernet/sing-box/common/urltest"
	dns "github.com/sagernet/sing-dns"
	E "github.com/sagernet/sing/common/exceptions"
	N "github.com/sagernet/sing/common/network"
	"github.com/sagernet/sing/common/varbin"
)

type ClashServer interface {
	Service
	PreStarter
	Mode() string
	ModeList() []string
	HistoryStorage() *urltest.HistoryStorage
	RoutedConnection(ctx context.Context, conn net.Conn, metadata InboundContext, matchedRule Rule) (net.Conn, Tracker)
	RoutedPacketConnection(ctx context.Context, conn N.PacketConn, metadata InboundContext, matchedRule Rule) (N.PacketConn, Tracker)
}

type CacheFile interface {
	Service
	PreStarter

	StoreFakeIP() bool
	FakeIPStorage

	StoreRDRC() bool
	dns.RDRCStore

	LoadMode() string
	StoreMode(mode string) error
	LoadSelected(group string) string
	StoreSelected(group string, selected string) error
	LoadGroupExpand(group string) (isExpand bool, loaded bool)
	StoreGroupExpand(group string, expand bool) error
	LoadRuleSet(tag string) *SavedRuleSet
	SaveRuleSet(tag string, set *SavedRuleSet) error
}

type SavedRuleSet struct {
	Content     []byte
	LastUpdated time.Time
	LastEtag    string
}

func (s *SavedRuleSet) MarshalBinary() ([]byte, error) {
	var buffer bytes.Buffer
	err := binary.Write(&buffer, binary.BigEndian, uint8(1))
	if err != nil {
		return nil, err
	}
	err = varbin.Write(&buffer, binary.BigEndian, s.Content)
	if err != nil {
		return nil, err
	}
	err = binary.Write(&buffer, binary.BigEndian, s.LastUpdated.Unix())
	if err != nil {
		return nil, err
	}
	err = varbin.Write(&buffer, binary.BigEndian, s.LastEtag)
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (s *SavedRuleSet) UnmarshalBinary(data []byte) error {
	reader := bytes.NewReader(data)
	var version uint8
	err := binary.Read(reader, binary.BigEndian, &version)
	if err != nil {
		return err
	}
	err = varbin.Read(reader, binary.BigEndian, &s.Content)
	if err != nil {
		return err
	}
	var lastUpdated int64
	err = binary.Read(reader, binary.BigEndian, &lastUpdated)
	if err != nil {
		return err
	}
	s.LastUpdated = time.Unix(lastUpdated, 0)
	err = varbin.Read(reader, binary.BigEndian, &s.LastEtag)
	if err != nil {
		return err
	}
	return nil
}

type Tracker interface {
	Leave()
}

type Provider interface {
	Service
	Tag() string
	Update() error
	UpdatedAt() time.Time
	Wait()
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
}

type OutboundGroup interface {
	Outbound
	Now() string
	All() []string
	Outbounds() []Outbound
	Outbound(tag string) (Outbound, bool)
	Providers() []Provider
	Provider(tag string) (Provider, bool)
}

type OutboundCheckGroup interface {
	OutboundGroup
	CheckAll(ctx context.Context) (map[string]uint16, error)
	CheckProvider(ctx context.Context, tag string) (map[string]uint16, error)
	CheckOutbound(ctx context.Context, tag string) (uint16, error)
}

type V2RayServer interface {
	Service
	StatsService() V2RayStatsService
}

type V2RayStatsService interface {
	RoutedConnection(inbound string, outbound string, user string, conn net.Conn) net.Conn
	RoutedPacketConnection(inbound string, outbound string, user string, conn N.PacketConn) N.PacketConn
}

func RealOutbound(outbound Outbound) (Outbound, error) {
	if outbound == nil {
		return nil, nil
	}
	redirected := outbound
	nLoop := 0
	for {
		group, isGroup := redirected.(OutboundGroup)
		if !isGroup {
			return redirected, nil
		}
		nLoop++
		if nLoop > 100 {
			return nil, E.New("too deep or loop nesting of outbound groups")
		}
		var ok bool
		now := group.Now()
		redirected, ok = group.Outbound(now)
		if !ok {
			return nil, E.New("outbound not found:", now)
		}
	}
}
