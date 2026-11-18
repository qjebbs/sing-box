package portprobe

import (
	"context"
	"io"
	"net"
	"strconv"

	E "github.com/sagernet/sing/common/exceptions"
)

// verifyHTTP sends a minimal HTTP request and waits for any response, which
// proves that a real server accepted the connection instead of an interceptor
// that completes the handshake for every destination.
func verifyHTTP(conn net.Conn, host string, ctx context.Context) error {
	if deadline, loaded := ctx.Deadline(); loaded {
		_ = conn.SetDeadline(deadline)
	}
	request := "GET / HTTP/1.0\r\nHost: " + host + "\r\nUser-Agent: sing-box\r\n\r\n"
	_, err := io.WriteString(conn, request)
	if err != nil {
		return E.Cause(err, "write request")
	}
	response := make([]byte, 1)
	_, err = io.ReadFull(conn, response)
	if err != nil {
		return E.Cause(err, "read response")
	}
	return nil
}

var _ = strconv.Itoa
