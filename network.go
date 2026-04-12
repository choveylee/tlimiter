package tlimiter

import (
	"net"
	"net/http"
	"strings"
)

var (
	// DefaultIPv4Mask is the default /32 mask applied to IPv4 addresses for masked keys.
	DefaultIPv4Mask = net.CIDRMask(32, 32)
	// DefaultIPv6Mask is the default /128 mask applied to IPv6 addresses for masked keys.
	DefaultIPv6Mask = net.CIDRMask(128, 128)
)

// GetIP returns the client IP address for r using the receiver's [Options].
func (limiter *Limiter) GetIP(r *http.Request) net.IP {
	return GetIP(r, limiter.Options)
}

// GetIPWithMask returns the client IP for r after applying the IPv4 or IPv6 mask from the receiver's [Options].
func (limiter *Limiter) GetIPWithMask(r *http.Request) net.IP {
	return GetIPWithMask(r, limiter.Options)
}

// GetIPKey returns the string representation of the masked client IP (see GetIPWithMask) for use as a store key.
func (limiter *Limiter) GetIPKey(r *http.Request) string {
	return limiter.GetIPWithMask(r).String()
}

// GetIP resolves the client IP for r. If options is non-empty, [Options.ClientIPHeader] and
// [Options.TrustForwardHeader] are applied before using the connection remote address.
func GetIP(r *http.Request, options ...Options) net.IP {
	if len(options) >= 1 {
		if options[0].ClientIPHeader != "" {
			ip := getIPFromHeader(r, options[0].ClientIPHeader)
			if ip != nil {
				return ip
			}
		}
		if options[0].TrustForwardHeader {
			ip := getIPFromXFFHeader(r)
			if ip != nil {
				return ip
			}

			ip = getIPFromHeader(r, "X-Real-IP")
			if ip != nil {
				return ip
			}
		}
	}

	remoteAddr := strings.TrimSpace(r.RemoteAddr)
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return net.ParseIP(remoteAddr)
	}

	return net.ParseIP(host)
}

// GetIPWithMask returns the masked client IP: it calls [GetIP] with the same arguments, then applies
// IPv4Mask or IPv6Mask from the first [Options] when options is non-empty.
// If options is empty, it returns the result of GetIP with no options (no masking).
func GetIPWithMask(r *http.Request, options ...Options) net.IP {
	if len(options) == 0 {
		return GetIP(r)
	}

	ip := GetIP(r, options[0])
	if ip.To4() != nil {
		return ip.Mask(options[0].IPv4Mask)
	}
	if ip.To16() != nil {
		return ip.Mask(options[0].IPv6Mask)
	}
	return ip
}

// getIPFromXFFHeader parses the first valid IP in the X-Forwarded-For list (left to right).
func getIPFromXFFHeader(r *http.Request) net.IP {
	headers := r.Header.Values("X-Forwarded-For")
	if len(headers) == 0 {
		return nil
	}

	parts := []string{}
	for _, header := range headers {
		parts = append(parts, strings.Split(header, ",")...)
	}

	for i := range parts {
		part := strings.TrimSpace(parts[i])
		ip := net.ParseIP(part)
		if ip != nil {
			return ip
		}
	}

	return nil
}

// getIPFromHeader returns the IP parsed from header name, or nil if missing or invalid.
func getIPFromHeader(r *http.Request, name string) net.IP {
	header := strings.TrimSpace(r.Header.Get(name))
	if header == "" {
		return nil
	}

	ip := net.ParseIP(header)
	if ip != nil {
		return ip
	}

	return nil
}
