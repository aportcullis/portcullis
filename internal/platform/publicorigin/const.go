package publicorigin

// Default ports a browser omits from an origin and may still send in a Host header.
const (
	defaultHTTPPort  = "80"
	defaultHTTPSPort = "443"
)

// maxPortNumber is the largest TCP port an origin may name.
const maxPortNumber = 65535

// loopbackHostname is the only DNS name the loopback default admits; loopback IP literals are recognized separately.
const loopbackHostname = "localhost"
