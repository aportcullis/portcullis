package health

// response is the JSON body for the liveness and readiness probes.
type response struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks,omitempty"`
}
