package tlimiter

import (
	"net"
)

// Option configures a [Limiter] during construction.
type Option func(*Options)

// Options configures client IP extraction and subnet masking for limiter keys.
type Options struct {
	// IPv4Mask is applied to IPv4 addresses when forming masked client keys.
	IPv4Mask net.IPMask
	// IPv6Mask is applied to IPv6 addresses when forming masked client keys.
	IPv6Mask net.IPMask
	// TrustForwardHeader enables reading X-Forwarded-For and X-Real-IP before
	// falling back to the direct remote address.
	TrustForwardHeader bool
	// ClientIPHeader, when non-empty, names a header that takes precedence over
	// TrustForwardHeader.
	ClientIPHeader string
}

// WithIPv4Mask returns an [Option] that sets the IPv4 mask used for masked
// client IP keys.
func WithIPv4Mask(mask net.IPMask) Option {
	return func(o *Options) {
		o.IPv4Mask = mask
	}
}

// WithIPv6Mask returns an [Option] that sets the IPv6 mask used for masked
// client IP keys.
func WithIPv6Mask(mask net.IPMask) Option {
	return func(o *Options) {
		o.IPv6Mask = mask
	}
}

// WithTrustForwardHeader returns an [Option] that enables trust in
// X-Forwarded-For and X-Real-IP.
//
// This option is unsafe unless an upstream reverse proxy sanitizes those
// headers before forwarding the request. See the package documentation for
// additional guidance.
func WithTrustForwardHeader(enable bool) Option {
	return func(o *Options) {
		o.TrustForwardHeader = enable
	}
}

// WithClientIPHeader returns an [Option] that reads the client IP from the
// named header.
//
// When header is non-empty, it takes precedence over
// [WithTrustForwardHeader]. See the package documentation for security notes.
func WithClientIPHeader(header string) Option {
	return func(o *Options) {
		o.ClientIPHeader = header
	}
}
