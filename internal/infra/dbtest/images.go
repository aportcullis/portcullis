package dbtest

import (
	"fmt"
	"os"
)

// PostgresTestImage selects only a reviewed immutable image for compatibility tests.
func PostgresTestImage() (string, error) {
	family := os.Getenv("PORTCULLIS_TEST_POSTGRES_FAMILY")
	switch family {
	case "", "18":
		return PostgresImage, nil
	case "16":
		return "postgres:16.15-alpine3.24@sha256:721873c34ceb9f8d8fc265984940dc982404c105f19ad51be9fdc5970a6080ea", nil
	case "17":
		return "postgres:17.11-alpine3.24@sha256:b0f9560a2de083e2cc7382e75f808c7381a32852a7ec49117deedb300e552b24", nil
	case "19":
		return "postgres:19beta4@sha256:d4afb1c70ecbcc4d87b8f7e86750a1a9ba04ddf830dbd27600ce14e5220077f6", nil
	default:
		return "", fmt.Errorf("unqualified PostgreSQL test family %q", family)
	}
}
