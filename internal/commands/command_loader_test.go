package commands_test

import (
	"testing"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
)

// MockCommand is a mock implementation of the commands.Command interface for testing.
type MockCommand struct {
	name        string
	description string
	options     []discord.CommandOption                                                                          // Corrected type
	executeFunc func(s *session.Session, i *gateway.InteractionCreateEvent, d *discord.CommandInteraction) error // Corrected signature
}

func (mc *MockCommand) Name() string {
	return mc.name
}

func (mc *MockCommand) Description() string {
	return mc.description
}

func (mc *MockCommand) Options() []discord.CommandOption { // Corrected return type
	return mc.options
}

func (mc *MockCommand) Execute(s *session.Session, i *gateway.InteractionCreateEvent, d *discord.CommandInteraction) error { // Corrected signature
	if mc.executeFunc != nil {
		return mc.executeFunc(s, i, d)
	}
	return nil
}

func TestNewCommandManager(t *testing.T) {
	appID := discord.AppID(12345)

	t.Run("SuccessWithUniqueCommands", func(t *testing.T) {
		cmd1 := &MockCommand{name: "ping", description: "Ping command"}
		cmd2 := &MockCommand{name: "help", description: "Help command"}
		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{cmd1, cmd2},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmd1, ok := cm.GetCommand("ping")
		assert.True(t, ok)
		assert.Equal(t, cmd1, retCmd1)

		retCmd2, ok := cm.GetCommand("help")
		assert.True(t, ok)
		assert.Equal(t, cmd2, retCmd2)

		_, ok = cm.GetCommand("nonexistent")
		assert.False(t, ok)
	})

	t.Run("NoCommands", func(t *testing.T) {
		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		_, ok := cm.GetCommand("any")
		assert.False(t, ok)
	})

	t.Run("NilCommandInSlice", func(t *testing.T) {
		cmd1 := &MockCommand{name: "valid", description: "A valid command"}
		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{nil, cmd1, nil},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmd1, ok := cm.GetCommand("valid")
		assert.True(t, ok)
		assert.Equal(t, cmd1, retCmd1)

		_, ok = cm.GetCommand("nil") // Ensure nil commands are not somehow registered
		assert.False(t, ok)
	})

	t.Run("DuplicateCommandNames", func(t *testing.T) {
		cmd1a := &MockCommand{name: "dup", description: "First duplicate"}
		cmd1b := &MockCommand{name: "dup", description: "Second duplicate"}
		cmd2 := &MockCommand{name: "unique", description: "Unique command"}
		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{cmd1a, cmd1b, cmd2},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmdDup, ok := cm.GetCommand("dup")
		assert.True(t, ok)
		assert.Equal(t, cmd1a, retCmdDup) // Should be the first one registered
		assert.NotEqual(t, cmd1b, retCmdDup)

		retCmdUnique, ok := cm.GetCommand("unique")
		assert.True(t, ok)
		assert.Equal(t, cmd2, retCmdUnique)
	})

	t.Run("NilLogger", func(t *testing.T) {
		cmd1 := &MockCommand{name: "testlog", description: "Test with nil logger"}
		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        nil, // Explicitly nil
			Commands:      []commands.Command{cmd1},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm) // Should default to zap.NewNop()

		retCmd1, ok := cm.GetCommand("testlog")
		assert.True(t, ok)
		assert.Equal(t, cmd1, retCmd1)
	})
}
