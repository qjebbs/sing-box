package link

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
)

func init() {
	common.Must(RegisterParser(&Parser{
		Name:   "Quantumult",
		Scheme: []string{"vmess"},
		Parse: func(u *url.URL) (Link, error) {
			return ParseVMessQuantumult(u)
		},
	}))
}

// VMessQuantumult is the vmess link of Quantumult
type VMessQuantumult struct {
	Vmess
}

// ParseVMessQuantumult parses a Quantumult vmess link
func ParseVMessQuantumult(u *url.URL) (*VMessQuantumult, error) {
	link := &VMessQuantumult{}
	if u.Scheme != "vmess" {
		return nil, E.New("not a vmess link")
	}
	b, err := base64Decode(u.Host + u.Path)
	if err != nil {
		return nil, err
	}

	// ps = vmess,192.168.100.1,443,aes-128-gcm,"uuid",over-tls=true,certificate=0,obfs=ws,obfs-path="/path",obfs-header="Host:host[Rr][Nn]whatever
	info := string(b)

	psn := strings.SplitN(info, " = ", 2)
	if len(psn) != 2 {
		return nil, ErrBadFormat
	}

	link.Tag = psn[0]
	params := strings.Split(psn[1], ",")
	port, err := strconv.ParseUint(params[2], 10, 16)
	if err != nil {
		return nil, E.Cause(err, "invalid port")
	}
	link.Server = params[1]
	link.ServerPort = uint16(port)
	link.Security = params[3]
	link.UUID = strings.Trim(params[4], "\"")
	link.AlterID = 0
	link.Transport = ""

	if len(params) > 4 {
		for _, param := range params[5:] {
			kvp := strings.SplitN(param, "=", 2)
			switch kvp[0] {
			case "over-tls":
				if len(kvp) != 2 {
					return nil, ErrBadFormat
				}
				if kvp[1] == "true" {
					link.TLS = true
				}
			case "obfs":
				if len(kvp) != 2 {
					return nil, ErrBadFormat
				}
				switch kvp[1] {
				case "ws", "websocket":
					link.Transport = C.V2RayTransportTypeWebsocket
				case "http", "h2":
					link.Transport = C.V2RayTransportTypeHTTP
				case "quci":
					link.Transport = C.V2RayTransportTypeQUIC
				case "grpc":
					link.Transport = C.V2RayTransportTypeGRPC
				default:
					return nil, fmt.Errorf("unsupported obfs parameter: %s", kvp[1])
				}
			case "obfs-path":
				if len(kvp) != 2 {
					return nil, ErrBadFormat
				}
				link.TransportPath = strings.Trim(kvp[1], "\"")
			case "obfs-header":
				if len(kvp) != 2 {
					return nil, ErrBadFormat
				}
				hd := strings.Trim(kvp[1], "\"")
				for _, hl := range strings.Split(hd, "[Rr][Nn]") {
					if strings.HasPrefix(hl, "Host:") {
						link.TransportHost = hl[5:]
						break
					}
				}
			case "certificate":
				if len(kvp) != 2 {
					return nil, ErrBadFormat
				}
				switch kvp[1] {
				case "0":
					link.TLSAllowInsecure = true
				default:
					link.TLSAllowInsecure = false
				}
			default:
				return nil, fmt.Errorf("unsupported parameter: %s", param)
			}
		}
	}
	return link, nil
}
