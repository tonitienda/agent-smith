package schema

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"
)

// idPrefix prefixes generated block IDs so they are recognizable on the log.
const idPrefix = "blk_"

// NewID returns a fresh, unique block ID. IDs are opaque, never reused, and are
// Agent Smith's own (not a provider's). Uniqueness comes from 128 bits of
// cryptographic randomness; "stable" is a usage contract (once assigned to a
// block, the ID is never changed), not a property of generation.
func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand should not fail on a healthy system; degrade to a
		// time-based identifier rather than fail block creation.
		return fmt.Sprintf("%s%016x", idPrefix, time.Now().UnixNano())
	}
	return idPrefix + hex.EncodeToString(b)
}
