package security

import (
	"net"
	"net/url"
)

// privateIPBlocks 私有 IP 段
var privateIPBlocks []*net.IPNet

func init() {
	for _, cidr := range []string{
		"127.0.0.0/8",   // IPv4 loopback
		"10.0.0.0/8",    // RFC1918 private A
		"172.16.0.0/12", // RFC1918 private B
		"192.168.0.0/16", // RFC1918 private C
		"::1/128",       // IPv6 loopback
		"fc00::/7",      // IPv6 unique local
		"fe80::/10",     // IPv6 link-local
	} {
		_, block, _ := net.ParseCIDR(cidr)
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

// isPrivateIP 检查 IP 是否为私有地址
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsUnspecified() {
		return true
	}
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// ValidateURLTarget 验证 URL 的 scheme 和域名（不解析 IP）
func ValidateURLTarget(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, "invalid URL: " + err.Error()
	}

	// 只允许 http 和 https
	if u.Scheme != "http" && u.Scheme != "https" {
		return false, "only http/https allowed, got '" + u.Scheme + "'"
	}

	// 必须有域名
	if u.Host == "" {
		return false, "missing domain"
	}

	// 检查域名是否为 localhost 或 IP
	host := u.Hostname()
	if host == "localhost" || host == "[::1]" {
		return false, "localhost not allowed"
	}

	// 检查是否为裸 IP 地址
	if net.ParseIP(host) != nil {
		return false, "direct IP address not allowed"
	}

	return true, ""
}

// ValidateResolvedURL 验证重定向后的 URL，解析 IP 并检查是否为私有地址
func ValidateResolvedURL(rawURL string) (bool, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, "invalid URL: " + err.Error()
	}

	host := u.Hostname()
	if host == "" {
		return false, "missing host"
	}

	// 解析域名获取 IP
	ips, err := net.LookupIP(host)
	if err != nil {
		return false, "DNS lookup failed: " + err.Error()
	}

	if len(ips) == 0 {
		return false, "no IP found for host"
	}

	// 检查所有解析的 IP
	for _, ip := range ips {
		if isPrivateIP(ip) {
			return false, "private IP not allowed: " + ip.String()
		}
	}

	return true, ""
}
