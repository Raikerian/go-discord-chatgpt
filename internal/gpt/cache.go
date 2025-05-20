package gpt

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sashabaranov/go-openai"
)

// IgnoredChannelsCache can be used to store a set of channel IDs where caching is disabled.
type IgnoredChannelsCache map[string]struct{}

// MessagesCache holds the LRU cache for chat messages.
type MessagesCache struct {
	*lru.Cache[string, *MessagesCacheData]
}

// MessagesCacheData stores the data for a cached message entry.
type MessagesCacheData struct {
	Messages      []openai.ChatCompletionMessage
	SystemMessage *openai.ChatCompletionMessage
	Model         string
	Temperature   *float32
	TokenCount    int
}

// NewMessagesCache creates a new MessagesCache with the given size.
// The size parameter determines the maximum number of items the cache can hold.
func NewMessagesCache(size int) (*MessagesCache, error) {
	if size <= 0 {
		// Or return a default size, or an error indicating invalid size
		// For now, let's ensure lru.New doesn't panic with non-positive size.
		// Hashicorp's LRU might handle this, but good to be explicit.
		// According to hashicorp/golang-lru docs, New() returns error if size is not positive.
		// So, this check is redundant if we directly pass size, but good for clarity if we were to default.
	}
	lruCache, err := lru.New[string, *MessagesCacheData](size)
	if err != nil {
		return nil, err
	}

	return &MessagesCache{
		Cache: lruCache,
	}, nil
}

// Add adds a value to the cache.
func (mc *MessagesCache) Add(key string, value *MessagesCacheData) {
	mc.Cache.Add(key, value)
}

// Get looks up a key's value from the cache.
func (mc *MessagesCache) Get(key string) (*MessagesCacheData, bool) {
	return mc.Cache.Get(key)
}

// Remove removes the provided key from the cache.
func (mc *MessagesCache) Remove(key string) {
	mc.Cache.Remove(key)
}

// Purge is used to completely clear the cache.
func (mc *MessagesCache) Purge() {
	mc.Cache.Purge()
}

// Len returns the number of items in the cache.
func (mc *MessagesCache) Len() int {
	return mc.Cache.Len()
}
