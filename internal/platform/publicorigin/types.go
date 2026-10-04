package publicorigin

// Policy is the immutable set of browser origins operators publish Portcullis under; the zero configuration admits only loopback hosts (ADR-0052).
type Policy struct {
	// origins holds normalized scheme://host[:port] values in configuration order.
	origins []string
	// authorities holds every accepted Host header value derived from origins, including explicit default ports.
	authorities map[string]bool
}
