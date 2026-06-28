package identity

// Permission is an atomic capability key in Google-IAM style "resource.verb"
// (e.g. "connections.get", "requests.approve"; verbs list/get/create/update/
// delete plus resource-specific actions). The fine-grained catalog, the system
// roles, and their permission assignments live in SQL (seeded) and are loaded at
// startup — they are not hardcoded in Go. A specific key is referenced only at
// the site that enforces it (ADR-0008).
type Permission string
