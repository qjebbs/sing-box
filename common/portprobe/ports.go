package portprobe

import (
	"strconv"
	"strings"

	E "github.com/sagernet/sing/common/exceptions"
)

// servicePort is a candidate port with the service usually listening on it.
type servicePort struct {
	port    int
	service string
}

// commonPorts are the default candidates, ordered by how widely the service is
// deployed: the ports a policy is most likely to allow are probed first.
var commonPorts = []servicePort{
	{443, "https"},
	{80, "http"},
	{53, "dns"},
	{22, "ssh"},
	{8080, "http-alt"},
	{8443, "https-alt"},
	{8000, "http-alt"},
	{25, "smtp"},
	{587, "submission"},
	{465, "smtps"},
	{110, "pop3"},
	{143, "imap"},
	{993, "imaps"},
	{995, "pop3s"},
	{21, "ftp"},
	{853, "dot"},
	{5353, "mdns"},
	{3306, "mysql"},
	{5432, "postgres"},
	{1433, "mssql"},
	{6379, "redis"},
}

// CommonPorts are the ports probed by default.
var CommonPorts = commonPortList()

// portNames maps a port to the service usually listening on it. The table is
// local instead of being read from /etc/services, which does not exist on every
// platform and is not the same everywhere.
var portNames = servicePortNames()

func commonPortList() []int {
	ports := make([]int, len(commonPorts))
	for index, entry := range commonPorts {
		ports[index] = entry.port
	}
	return ports
}

func servicePortNames() map[int]string {
	names := make(map[int]string, len(commonPorts))
	for _, entry := range commonPorts {
		names[entry.port] = entry.service
	}
	return names
}

// PortName returns the service usually listening on the port, or an empty string
// when the port is not a known one.
func PortName(port int) string {
	return portNames[port]
}

// ParsePorts parses a port specification into an ordered port list.
//
// The specification is a comma separated list of ports and ranges, for example
// "443,8080,10000-20000". The keyword "common" expands to CommonPorts, and it is
// also used when the specification is empty.
func ParsePorts(specification string) ([]int, error) {
	specification = strings.TrimSpace(specification)
	if specification == "" || strings.EqualFold(specification, "common") {
		return append([]int(nil), CommonPorts...), nil
	}
	var ports []int
	seen := make(map[int]struct{})
	add := func(port int) {
		if _, loaded := seen[port]; loaded {
			return
		}
		seen[port] = struct{}{}
		ports = append(ports, port)
	}
	for _, part := range strings.Split(specification, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.EqualFold(part, "common") {
			for _, port := range CommonPorts {
				add(port)
			}
			continue
		}
		low, high, isRange := strings.Cut(part, "-")
		if !isRange {
			port, err := parsePort(part)
			if err != nil {
				return nil, err
			}
			add(port)
			continue
		}
		lowPort, err := parsePort(low)
		if err != nil {
			return nil, err
		}
		highPort, err := parsePort(high)
		if err != nil {
			return nil, err
		}
		if lowPort > highPort {
			return nil, E.New("invalid port range: ", part)
		}
		for port := lowPort; port <= highPort; port++ {
			add(port)
		}
	}
	if len(ports) == 0 {
		return nil, E.New("no ports specified")
	}
	return ports, nil
}

func parsePort(value string) (int, error) {
	port, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, E.Cause(err, "invalid port: ", value)
	}
	if port < 1 || port > 65535 {
		return 0, E.New("invalid port: ", value)
	}
	return port, nil
}
