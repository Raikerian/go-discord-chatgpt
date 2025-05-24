// Package gpt provides caching utilities for OpenAI GPT interactions and conversation management.
package gpt

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sashabaranov/go-openai"
	"go.uber.org/fx"
)

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

// MessagesCacheParams holds the dependencies for creating a new MessagesCache.
type MessagesCacheParams struct {
	fx.In
	Size int `name:"messageCacheSize"`
}

// NewMessagesCache creates a new MessagesCache with the given size.
// The size parameter determines the maximum number of items the cache can hold.
func NewMessagesCache(params MessagesCacheParams) (*MessagesCache, error) {
	lruCache, err := lru.New[string, *MessagesCacheData](params.Size)
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
