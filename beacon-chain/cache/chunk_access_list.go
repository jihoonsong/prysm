package cache

import (
	"sync"

	"github.com/OffchainLabs/prysm/v6/consensus-types/primitives"
)

type ChunkAccessListCache struct {
	mu   sync.RWMutex
	cals map[primitives.Slot]map[primitives.ChunkIndex][][]byte
}

// NewChunkAccessListCache initializes a new ChunkAccessListCache instance.
func NewChunkAccessListCache() *ChunkAccessListCache {
	return &ChunkAccessListCache{
		cals: make(map[primitives.Slot]map[primitives.ChunkIndex][][]byte),
	}
}

// Add adds a chunk access list.
func (c *ChunkAccessListCache) Add(slot primitives.Slot, chunkIndex primitives.ChunkIndex, chunkAccessList [][]byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cals[slot]; !ok {
		c.cals[slot] = make(map[primitives.ChunkIndex][][]byte)
	}

	c.cals[slot][chunkIndex] = chunkAccessList
}

// Get retrieves chunk access lists for a specific slot.
func (c *ChunkAccessListCache) Get(slot primitives.Slot, chunkIndex primitives.ChunkIndex) [][]byte {
	c.mu.RLock()
	defer c.mu.RUnlock()

	cal, exists := c.cals[slot][chunkIndex]
	if !exists {
		return nil
	}

	return cal
}

// Delete removes all chunk access lists for a specific slot.
func (c *ChunkAccessListCache) Delete(slot primitives.Slot) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.cals, slot)
}
