package config

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

type Pool struct {
	Label string   `json:"label"`
	IPs   []string `json:"ips"`
}

// Expand supports individual IPv4 addresses, CIDR, wildcards and /24 shorthand ranges.
func Expand(input string) ([]Pool, []string, error) {
	pools := []Pool{}
	ips := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.FieldsFunc(input, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' }) {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		items, err := expandOne(text)
		if err != nil {
			return nil, nil, err
		}
		pool := Pool{Label: text, IPs: []string{}}
		for _, ip := range items {
			if !seen[ip] {
				seen[ip] = true
				ips = append(ips, ip)
				pool.IPs = append(pool.IPs, ip)
			}
			if len(ips) > 512 {
				return nil, nil, fmt.Errorf("не более 512 уникальных IP")
			}
		}
		pools = append(pools, pool)
	}
	if len(ips) == 0 {
		return nil, nil, fmt.Errorf("укажите хотя бы один IPv4-адрес или диапазон")
	}
	return pools, ips, nil
}

func expandOne(text string) ([]string, error) {
	bad := func() ([]string, error) {
		return nil, fmt.Errorf("некорректный диапазон: %s", text)
	}
	value := text
	if strings.HasSuffix(value, ".*") {
		value = strings.TrimSuffix(value, "*") + "0-255"
	}
	if strings.Contains(value, "-") {
		if strings.Contains(value, "/") {
			if !strings.HasSuffix(value, "/24") {
				return bad()
			}
			value = strings.TrimSuffix(value, "/24")
		}
		parts := strings.Split(value, "-")
		if len(parts) != 2 {
			return bad()
		}
		start, err := netip.ParseAddr(strings.TrimSpace(parts[0]))
		if err != nil || !start.Is4() {
			return bad()
		}
		endText := strings.TrimSpace(parts[1])
		if !strings.Contains(endText, ".") {
			endText = value[:strings.LastIndex(parts[0], ".")+1] + endText
		}
		end, err := netip.ParseAddr(endText)
		if err != nil || !end.Is4() || end.Less(start) {
			return bad()
		}
		out := []string{}
		for ip := start; ip.IsValid() && !end.Less(ip); ip = ip.Next() {
			out = append(out, ip.String())
			if len(out) > 512 {
				return nil, fmt.Errorf("слишком большой диапазон: %s", text)
			}
		}
		return out, nil
	}
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() < 23 {
			return bad()
		}
		out := []string{}
		for ip := prefix.Masked().Addr(); prefix.Contains(ip); ip = ip.Next() {
			out = append(out, ip.String())
		}
		// Exclude network and broadcast for conventional subnets, keep both addresses in /31.
		if prefix.Bits() <= 30 {
			out = out[1 : len(out)-1]
		}
		return out, nil
	}
	ip, err := netip.ParseAddr(value)
	if err != nil || !ip.Is4() {
		return bad()
	}
	return []string{ip.String()}, nil
}

func Match(pattern, ip string) bool {
	_, ips, err := Expand(pattern)
	if err != nil {
		return false
	}
	for _, candidate := range ips {
		if candidate == ip {
			return true
		}
	}
	return false
}

func Port(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("порт должен быть от 1 до 65535")
	}
	return n, nil
}
