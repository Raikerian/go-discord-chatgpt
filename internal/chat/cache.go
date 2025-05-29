// Package chat provides caching utilities for OpenAI chat interactions and conversation management.
package chat

import (
	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/sashabaranov/go-openai"
)

// MessagesCacheData stores the data for a cached message entry.
type MessagesCacheData struct {
	Messages      []openai.ChatCompletionMessage
	SystemMessage *openai.ChatCompletionMessage
	Model         string
	Temperature   *float32
	TokenCount    int
}

// NewMessagesCache creates a new LRU cache for chat messages with the given size.
// The size parameter determines the maximum number of items the cache can hold.
func NewMessagesCache(size int) *lru.Cache[string, *MessagesCacheData] {
	lruCache, err := lru.New[string, *MessagesCacheData](size)
	if err != nil {
		// This should never happen with a valid size, but we'll panic if it does
		// since this is a programming error
		panic(err)
	}

	return lruCache
}

// NewNegativeThreadCache creates a new LRU cache for thread IDs that should be ignored.
// The size parameter determines the maximum number of items the cache can hold.
func NewNegativeThreadCache(size int) *lru.Cache[string, bool] {
	lruCache, err := lru.New[string, bool](size)
	if err != nil {
		// This should never happen with a valid size, but we'll panic if it does
		// since this is a programming error
		panic(err)
	}

	return lruCache
}
