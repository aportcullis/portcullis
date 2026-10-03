package keyrotation

import "errors"

// ErrIncomplete refuses completion while any encrypted envelope uses a nonactive key.
var ErrIncomplete = errors.New("keyrotation: unresolved encryption versions; retain all historical keys")
