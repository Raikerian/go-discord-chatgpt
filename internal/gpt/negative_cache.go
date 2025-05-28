package gpt

import (
	lru "github.com/hashicorp/golang-lru/v2"
)

// NegativeThreadCache holds the LRU cache for thread IDs that should be ignored.
type NegativeThreadCache struct {
	*lru.Cache[string, bool] // Using string for thread ID and bool as a placeholder value
}

// NewNegativeThreadCache creates a new NegativeThreadCache with the given size.
// The size parameter determines the maximum number of items the cache can hold.
func NewNegativeThreadCache(size int) NegativeThreadCache {
	lruCache, err := lru.New[string, bool](size)
	if err != nil {
		// This should never happen with a valid size, but we'll panic if it does
		// since this is a programming error
		panic(err)
	}

	return NegativeThreadCache{
		Cache: lruCache,
	}
}

// Add adds a thread ID to the cache.
func (ntc *NegativeThreadCache) Add(threadID string) {
	ntc.Cache.Add(threadID, true)
}

// Contains checks if a thread ID is in the cache.
func (ntc *NegativeThreadCache) Contains(threadID string) bool {
	_, ok := ntc.Get(threadID)

	return ok
}

// Remove removes the provided thread ID from the cache.
func (ntc *NegativeThreadCache) Remove(threadID string) {
	ntc.Cache.Remove(threadID)
}

// Purge is used to completely clear the cache.
func (ntc *NegativeThreadCache) Purge() {
	ntc.Cache.Purge()
}

// Len returns the number of items in the cache.
func (ntc *NegativeThreadCache) Len() int {
	return ntc.Cache.Len()
}
