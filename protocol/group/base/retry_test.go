package base

import (
	"context"
	"slices"
	"testing"

	"github.com/sagernet/sing-box/log"
	E "github.com/sagernet/sing/common/exceptions"
)

type stubCandidate struct {
	tag string
}

func TestTryEach(t *testing.T) {
	t.Parallel()
	nodes := []*stubCandidate{{tag: "failed"}, {tag: "working"}}
	var observed []string
	cursor := 0
	value, err := tryEach(context.Background(), log.NewNOPFactory().Logger(),
		func() (*stubCandidate, bool) {
			if cursor >= len(nodes) {
				return nil, false
			}
			node := nodes[cursor]
			cursor++
			return node, true
		},
		func(node *stubCandidate) {
			// only the failures are reported, a success adds nothing to a probe
			observed = append(observed, node.tag)
		},
		func(node *stubCandidate) (string, error) {
			if node.tag == "failed" {
				return "", E.New("dial failed")
			}
			return node.tag, nil
		},
	)
	if err != nil {
		t.Fatalf("want no error, got %v", err)
	}
	if value != "working" {
		t.Fatalf("want working, got %s", value)
	}
	if !slices.Equal(observed, []string{"failed"}) {
		t.Fatalf("want the failed node reported, got %v", observed)
	}
}

func TestTryEachGivesUp(t *testing.T) {
	t.Parallel()
	attempts := 0
	_, err := tryEach(context.Background(), log.NewNOPFactory().Logger(),
		func() (*stubCandidate, bool) {
			return &stubCandidate{tag: "failed"}, true
		},
		func(_ *stubCandidate) {
			attempts++
		},
		func(*stubCandidate) (string, error) {
			return "", E.New("dial failed")
		},
	)
	if err == nil {
		t.Fatal("want an error")
	}
	if attempts != maxAttempts {
		t.Fatalf("want %v attempts, got %v", maxAttempts, attempts)
	}
}

func TestTryEachWithoutNodes(t *testing.T) {
	t.Parallel()
	_, err := tryEach(context.Background(), log.NewNOPFactory().Logger(),
		func() (*stubCandidate, bool) {
			return nil, false
		},
		func(*stubCandidate) {
			t.Fatal("no node can be reported")
		},
		func(*stubCandidate) (string, error) {
			t.Fatal("no node can be tried")
			return "", nil
		},
	)
	if err == nil {
		t.Fatal("want an error")
	}
}
