package turms

import (
	"math/rand"
	"sync"
)

type ID uint64

const (
	upperBoundID = 9007199254740992
)

// NewGlobalID draws a randomly, uniformly distributed ID from the complete range of [0, 2^53].
// The drawn ID is allowed to be used in the global scope.
func NewGlobalID() ID {
	return ID(rand.Int63n(upperBoundID))
}

type idCounter struct {
	id ID
	mu sync.Mutex
}

func (idc *idCounter) Next() ID {
	idc.mu.Lock()
	defer idc.mu.Unlock()
	id := idc.id
	v := uint64(idc.id) + 1
	if v == upperBoundID {
		v = 1
	}
	idc.id = ID(v)
	return id
}
