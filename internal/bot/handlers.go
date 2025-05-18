package bot

import (
	"log"

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
)

func handleInteraction(s *session.Session, e *gateway.InteractionCreateEvent) {
	// Check if it's a slash command
	switch data := e.Data.(type) {
	case *discord.CommandInteraction:
		log.Printf("Received slash command: %s", data.Name)

		// Get the command handler
		cmd, ok := commands.GetCommand(data.Name)
		if !ok {
			log.Printf("Unknown command: %s", data.Name)
			// Optionally send a response back to the user indicating the command is not found
			err := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("Command not found."),
				},
			})
			if err != nil {
				log.Printf("Failed to respond to interaction: %v", err)
			}
			return
		}

		// Execute the command
		err := cmd.Execute(s, e, data)
		if err != nil {
			log.Printf("Error executing command %s: %v", data.Name, err)
			// Optionally send an error response back to the user
			errResp := s.RespondInteraction(e.ID, e.Token, api.InteractionResponse{
				Type: api.MessageInteractionWithSource,
				Data: &api.InteractionResponseData{
					Content: option.NewNullableString("An error occurred while executing the command."),
				},
			})
			if errResp != nil {
				log.Printf("Failed to send error response: %v", errResp)
			}
		}

	default:
		log.Printf("Received unhandled interaction type: %T", e.Data)
	}
}
