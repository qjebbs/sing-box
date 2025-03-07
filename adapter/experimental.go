package adapter

import (
	"bytes"
	"context"
	"encoding/binary"
	"time"

	"github.com/sagernet/sing-box/common/urltest"
	dns "github.com/sagernet/sing-dns"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/varbin"
)

type ClashServer interface {
	LifecycleService
	ConnectionTracker
	Mode() string
	ModeList() []string
	HistoryStorage() *urltest.HistoryStorage
}

type V2RayServer interface {
	LifecycleService
	StatsService() ConnectionTracker
}

type CacheFile interface {
	LifecycleService

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
	LoadRuleSet(tag string) *SavedBinary
	SaveRuleSet(tag string, set *SavedBinary) error
}

type SavedBinary struct {
	Content     []byte
	LastUpdated time.Time
	LastEtag    string
}

func (s *SavedBinary) MarshalBinary() ([]byte, error) {
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

func (s *SavedBinary) UnmarshalBinary(data []byte) error {
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
