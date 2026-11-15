package tlimiter

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

var (
	// DefaultIPv4Mask is the default /32 mask applied to IPv4 client addresses.
	DefaultIPv4Mask = net.CIDRMask(32, 32)
	// DefaultIPv6Mask is the default /128 mask applied to IPv6 client addresses.
	DefaultIPv6Mask = net.CIDRMask(128, 128)
)

// GetIP returns the client IP address for request by using the receiver's [Options].
func (limiter *Limiter) GetIP(request *http.Request) net.IP {
	return GetIP(request, limiter.Options)
}

// GetIPWithMask returns the client IP for request after applying the IPv4 or IPv6
// mask from the receiver's [Options].
func (limiter *Limiter) GetIPWithMask(request *http.Request) net.IP {
	return GetIPWithMask(request, limiter.Options)
}

// GetIPKey returns the string form of the masked client IP resolved by
// [Limiter.GetIPWithMask], suitable for use as a store key.
func (limiter *Limiter) GetIPKey(request *http.Request) string {
	return limiter.GetIPWithMask(request).String()
}

// GetIP resolves the client IP address for request.
//
// The first options value controls resolution: a valid ClientIPHeader takes
// precedence, followed by X-Forwarded-For and X-Real-IP when TrustForwardHeader
// is enabled, then RemoteAddr. Invalid header values fall back to the next source.
// RemoteAddr supports scoped IPv6 addresses; returned IPs omit the zone.
// Headers must be sanitized by the upstream proxy; no trusted-proxy CIDR check
// is performed here. An absent or unparseable address returns nil.
func GetIP(request *http.Request, options ...Options) net.IP {
	if request == nil {
		return nil
	}

	if len(options) >= 1 {
		if options[0].ClientIPHeader != "" {
			header := strings.TrimSpace(request.Header.Get(options[0].ClientIPHeader))
			ip := net.ParseIP(header)
			if ip != nil {
				return ip
			}
		}

		if options[0].TrustForwardHeader {
			// Preserve the first valid address across all forwarded-header values.
			for _, header := range request.Header.Values("X-Forwarded-For") {
				for _, address := range strings.Split(header, ",") {
					ip := net.ParseIP(strings.TrimSpace(address))
					if ip != nil {
						return ip
					}
				}
			}

			header := strings.TrimSpace(request.Header.Get("X-Real-IP"))
			ip := net.ParseIP(header)
			if ip != nil {
				return ip
			}
		}
	}

	remoteAddr := strings.TrimSpace(request.RemoteAddr)
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		remoteAddr = host
	}

	address, err := netip.ParseAddr(remoteAddr)
	if err != nil {
		return nil
	}

	// net.IP keys use the address alone; As16 drops any zone and preserves IPv4 mapping.
	addressBytes := address.As16()

	return net.IP(addressBytes[:])
}

// GetIPWithMask resolves the client IP by calling [GetIP] with the same
// arguments and then applies the IPv4 or IPv6 mask from the first [Options]
// value when options is non-empty.
//
// If options is empty, the result of [GetIP] is returned without masking.
func GetIPWithMask(request *http.Request, options ...Options) net.IP {
	if len(options) == 0 {
		return GetIP(request)
	}

	ip := GetIP(request, options[0])
	if ip.To4() != nil {
		return ip.Mask(options[0].IPv4Mask)
	}

	if ip.To16() != nil {
		return ip.Mask(options[0].IPv6Mask)
	}

	return ip
}
