// Package idgen generates short, readable IDs prefixed by entity type
// (e.g. mem_xxxxxxxxxxxx), replacing the original memory-server's
// mem-<unixnano> as decided in the Phase 1 implementation plan.
package idgen

import (
	"crypto/rand"
)

const alphabet = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

// New generates an ID in the format "<prefix>_<12 random characters>".
func New(prefix string) string {
	b := make([]byte, 12)
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		panic("idgen: failed to read crypto/rand: " + err.Error())
	}
	for i, r := range random {
		b[i] = alphabet[int(r)%len(alphabet)]
	}
	return prefix + "_" + string(b)
}
