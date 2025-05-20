package chat

import (
	"fmt"
	"strings"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/session"
)

const (
	discordMaxMessageLength = 2000 // Define Discord's max message length
)

// MakeThreadName generates a suitable name for a Discord thread based on the user and prompt.
// It truncates the prompt part if the total length exceeds maxLength.
func MakeThreadName(username, prompt string, maxLength int) string {
	prefix := fmt.Sprintf("Chat with %s: ", username)
	if len(prompt) == 0 {
		prompt = "New Chat"
	}

	maxPromptLen := maxLength - len(prefix)

	if maxPromptLen <= 0 {
		if len(prefix) > maxLength {
			if maxLength <= 3 {
				return prefix[:maxLength]
			}
			return prefix[:maxLength-3] + "..."
		}
		return prefix
	}

	var truncatedPrompt string
	if len(prompt) > maxPromptLen {
		if maxPromptLen <= 3 {
			truncatedPrompt = prompt[:maxPromptLen]
		} else {
			truncatedPrompt = prompt[:maxPromptLen-3] + "..."
		}
	} else {
		truncatedPrompt = prompt
	}

	name := prefix + truncatedPrompt
	if len(name) > maxLength {
		// Ensure the final truncation with "..." still respects maxLength
		if maxLength <= 3 {
			return name[:maxLength]
		}
		return name[:maxLength-3] + "..."
	}
	return name
}

// SendLongMessage sends a message to a Discord channel, splitting it into multiple messages
// if it exceeds discordMaxMessageLength.
func SendLongMessage(s *session.Session, channelID discord.ChannelID, content string) error {
	if len(content) <= discordMaxMessageLength {
		_, err := s.Client.SendMessageComplex(channelID, api.SendMessageData{Content: content})
		return err
	}

	var parts []string
	remainingContent := content
	for len(remainingContent) > 0 {
		if len(remainingContent) <= discordMaxMessageLength {
			parts = append(parts, remainingContent)
			break
		}

		// Find a good place to split (e.g., newline, space) to avoid breaking words/sentences awkwardly.
		splitAt := discordMaxMessageLength
		// Try to split at the last newline within the limit
		lastNewline := strings.LastIndex(remainingContent[:splitAt], "\\n")
		if lastNewline != -1 && lastNewline > 0 { // lastNewline > 0 to ensure we don't create empty messages if it starts with \\n
			splitAt = lastNewline
		} else {
			// If no newline, try to split at the last space within the limit
			lastSpace := strings.LastIndex(remainingContent[:splitAt], " ")
			if lastSpace != -1 && lastSpace > 0 { // lastSpace > 0 to ensure we don't create empty messages
				splitAt = lastSpace
			}
			// If no space or newline, we have to split mid-word (or the message is one giant word)
		}

		parts = append(parts, strings.TrimSpace(remainingContent[:splitAt]))
		remainingContent = strings.TrimSpace(remainingContent[splitAt:])
	}

	for i, part := range parts {
		if strings.TrimSpace(part) == "" { // Avoid sending empty messages
			continue
		}
		_, err := s.Client.SendMessageComplex(channelID, api.SendMessageData{Content: part})
		if err != nil {
			return fmt.Errorf("failed to send message part %d/%d: %w", i+1, len(parts), err)
		}
	}
	return nil
}
