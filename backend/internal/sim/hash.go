package sim

import "crypto/sha256"

// Hash returns the SHA-256 of the canonical encoding of s (Req 12.1). It
// covers simulation state only (INV-15).
func Hash(s State) [32]byte { return sha256.Sum256(Encode(s)) }
