package link

import (
	"fmt"
	"strconv"
	"strings"
)

type HysteriaPortRange struct {
	Start uint16
	End   uint16
}

type HysteriaPortRanges []HysteriaPortRange

// ParsePortRanges parses port ranges string to PortRanges,
// example:
//
//	1234,5000-6000
//	1234,5000:6000
func ParsePortRanges(raw string) (HysteriaPortRanges, error) {
	var result HysteriaPortRanges
	parts := strings.Split(raw, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Try to parse as a range (separated by - or :)
		rangeDelim := strings.IndexAny(part, "-:")
		if rangeDelim >= 0 {
			startStr := strings.TrimSpace(part[:rangeDelim])
			endStr := strings.TrimSpace(part[rangeDelim+1:])

			var start, end uint16

			// Parse start port
			if startStr != "" {
				r, err := strconv.ParseUint(startStr, 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid port range: %s", part)
				}
				start = uint16(r)
			}

			// Parse end port
			if endStr != "" {
				r, err := strconv.ParseUint(endStr, 10, 16)
				if err != nil {
					return nil, fmt.Errorf("invalid port range: %s", part)
				}
				end = uint16(r)
			}

			result = append(result, HysteriaPortRange{Start: start, End: end})
		} else {
			// Single port
			port64, err := strconv.ParseUint(part, 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid port: %s", part)
			}
			port := uint16(port64)
			result = append(result, HysteriaPortRange{Start: port, End: port})
		}
	}
	return result, nil
}

func (r HysteriaPortRange) String() string {
	return r.Format("-")
}

func (r HysteriaPortRange) Format(delim string) string {
	if r.Start == 0 && r.End == 0 {
		return ""
	}
	if r.Start == 0 {
		return fmt.Sprintf("%d%s%d", r.End, delim, r.End)
	}
	if r.End == 0 {
		return fmt.Sprintf("%d%s%d", r.Start, delim, r.Start)
	}
	return fmt.Sprintf("%d%s%d", r.Start, delim, r.End)
}

func (r HysteriaPortRanges) String() string {
	sb := new(strings.Builder)
	for i, r := range r {
		if r.Start == 0 && r.End == 0 {
			continue
		}
		if i > 0 {
			sb.WriteRune(',')
		}
		sb.WriteString(r.Format("-"))
	}
	return sb.String()
}

func (r HysteriaPortRanges) SingBoxPorts() []string {
	var result = make([]string, 0, len(r))
	for _, r := range r {
		if r.Start != 0 && r.End != 0 {
			result = append(result, r.Format(":"))
		}
	}
	return result
}
