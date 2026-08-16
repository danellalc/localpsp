package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

// idLength is how many hex characters follow the prefix. 16 hex characters
// (64 bits of the seeded hash) is short enough to read in logs and long
// enough that collisions inside one engine instance are not a concern.
const idLength = 16

// idGenerator produces deterministic ids from a seed and an internal
// counter. The same seed, given the same sequence of next calls, always
// produces the same ids.
type idGenerator struct {
	seed    int64
	counter uint64
}

func newIDGenerator(seed int64) *idGenerator {
	return &idGenerator{seed: seed}
}

func (g *idGenerator) next(prefix string) string {
	return prefix + g.nextHex(idLength)
}

// nextHex returns the next n hex characters (n <= 64) from the same seeded
// counter stream as next, without a prefix. Lets a caller mint a longer
// deterministic token, still reproducible from the engine's seed.
func (g *idGenerator) nextHex(n int) string {
	g.counter++
	sum := sha256.Sum256([]byte(strconv.FormatInt(g.seed, 10) + ":" + strconv.FormatUint(g.counter, 10)))
	return hex.EncodeToString(sum[:])[:n]
}
