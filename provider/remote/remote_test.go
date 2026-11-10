package remote

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
)

// TestStateStaysReadableWhileDownloading checks that a slow subscription does not hold the
// nodes away from the connections: the download is the slow part of an update, and it is not
// done under the lock the state is read with. It also covers the start of the box, which waits
// for the start stage of the provider rather than for its fetch.
func TestStateStaysReadableWhileDownloading(t *testing.T) {
	t.Parallel()
	requested := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requested)
		<-release
	}))
	defer server.Close()
	provider := newTestRemote(server)
	updated := make(chan error, 1)
	go func() {
		updated <- provider.Update()
	}()
	<-requested

	// The subscription server has not answered yet: everything the connections read has to be
	// there all the same.
	read := make(chan struct{})
	go func() {
		provider.Outbounds()
		provider.Outbound("")
		provider.UpdatedAt()
		provider.Ready()
		close(read)
	}()
	select {
	case <-read:
	case <-time.After(5 * time.Second):
		t.Fatal("want the state readable while the subscription is downloading")
	}

	select {
	case <-provider.Ready():
		t.Fatal("want the provider not ready before the first fetch finished")
	default:
	}
	close(release)
	if err := <-updated; err != nil {
		t.Fatalf("update: %v", err)
	}
	select {
	case <-provider.Ready():
	default:
		t.Fatal("want the provider ready after the first fetch")
	}
}

func newTestRemote(server *httptest.Server) *Remote {
	return &Remote{
		options:    option.RemoteProviderOptions{URL: server.URL},
		ctx:        context.Background(),
		ready:      make(chan struct{}),
		logger:     log.NewNOPFactory().NewLogger("provider/remote[test]"),
		httpClient: server.Client(),
	}
}
