package interrupt_test

import (
	"net"
	"testing"

	"github.com/sagernet/sing-box/common/interrupt"
)

type stubConn struct {
	net.Conn
	closed bool
}

func (c *stubConn) Close() error {
	c.closed = true
	return nil
}

func TestTaggedGroupInterrupt(t *testing.T) {
	t.Parallel()
	group := interrupt.NewTaggedGroup()
	selected := &stubConn{}
	retainedExternal := &stubConn{}
	retainedInternal := &stubConn{}
	dropped := &stubConn{}
	group.NewConn(selected, true, "selected")
	group.NewConn(retainedExternal, true, "retained")
	group.NewConn(retainedInternal, false, "retained")
	group.NewConn(dropped, true, "dropped")
	group.Interrupt(true, []string{"selected"}, []string{"retained"})

	if selected.closed {
		t.Fatalf("the connection of a selected node was closed")
	}
	if retainedExternal.closed {
		t.Fatalf("the external connection of a retained node was closed")
	}
	if !retainedInternal.closed {
		t.Fatalf("the internal connection of a retained node was kept")
	}
	if !dropped.closed {
		t.Fatalf("the connection of a dropped node was kept")
	}
}

func TestTaggedGroupRetainedThenDropped(t *testing.T) {
	t.Parallel()
	group := interrupt.NewTaggedGroup()
	conn := &stubConn{}
	group.NewConn(conn, true, "retained")
	// kept once, then it leaves the retention too: it must be closed the next time
	group.Interrupt(true, nil, []string{"retained"})
	if conn.closed {
		t.Fatalf("the connection was closed while retained")
	}
	group.Interrupt(true, nil, nil)
	if !conn.closed {
		t.Fatalf("the connection was kept after the retention ended")
	}
}

func TestTaggedGroupTags(t *testing.T) {
	t.Parallel()
	group := interrupt.NewTaggedGroup()
	group.NewConn(&stubConn{}, true, "a")
	group.NewConn(&stubConn{}, true, "b")
	tags := group.Tags()
	if len(tags) != 2 {
		t.Fatalf("want 2 tags, got %v", tags)
	}
	group.Interrupt(true, []string{"a"}, nil)
	tags = group.Tags()
	if len(tags) != 1 || tags[0] != "a" {
		t.Fatalf("want only the selected tag, got %v", tags)
	}
}

func TestTaggedGroupInterruptExternalDisabled(t *testing.T) {
	t.Parallel()
	group := interrupt.NewTaggedGroup()
	external := &stubConn{}
	internal := &stubConn{}
	group.NewConn(external, true, "dropped")
	group.NewConn(internal, false, "dropped")
	group.Interrupt(false, nil, nil)
	if external.closed {
		t.Fatalf("an external connection was closed while the option is disabled")
	}
	if !internal.closed {
		t.Fatalf("an internal connection was kept while the option is disabled")
	}
}

func TestTaggedGroupTagsSkipsClosed(t *testing.T) {
	t.Parallel()
	group := interrupt.NewTaggedGroup()
	conn := group.NewConn(&stubConn{}, true, "a")
	// kept for its connections, then the connection ends by itself
	group.Interrupt(true, nil, []string{"a"})
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	if tags := group.Tags(); len(tags) != 0 {
		t.Fatalf("a tag without a connection must not be reported, got %v", tags)
	}
}

func TestTaggedGroupRetentionIsIrrelevantWhenExternalDisabled(t *testing.T) {
	t.Parallel()
	// The retention only decides which external connections are kept, and with the
	// option off they are all kept anyway. Internal connections are interrupted
	// either way. This is why the load balancer does not compute it at all.
	external := &stubConn{}
	internal := &stubConn{}
	group := interrupt.NewTaggedGroup()
	group.NewConn(external, true, "retained")
	group.NewConn(internal, false, "retained")
	group.Interrupt(false, nil, []string{"retained"})
	if external.closed {
		t.Fatalf("an external connection was closed while the option is disabled")
	}
	if !internal.closed {
		t.Fatalf("an internal connection was kept, the option does not apply to it")
	}

	// the same node without the retention keeps exactly the same connections
	withoutRetention := &stubConn{}
	group = interrupt.NewTaggedGroup()
	group.NewConn(withoutRetention, true, "retained")
	group.Interrupt(false, nil, nil)
	if withoutRetention.closed {
		t.Fatalf("an external connection was closed while the option is disabled")
	}
}
