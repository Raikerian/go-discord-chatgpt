package commands_test

import (
	"testing"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/Raikerian/go-discord-chatgpt/internal/commands"
	"github.com/Raikerian/go-discord-chatgpt/pkg/test"
)

func TestNewCommandManager(t *testing.T) {
	appID := discord.AppID(12345)

	t.Run("SuccessWithUniqueCommands", func(t *testing.T) {
		mockCmd1 := test.NewMockCommand(t)
		mockCmd1.On("Name").Return("ping")

		mockCmd2 := test.NewMockCommand(t)
		mockCmd2.On("Name").Return("help")

		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{mockCmd1, mockCmd2},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmd1, ok := cm.GetCommand("ping")
		assert.True(t, ok)
		assert.Equal(t, mockCmd1, retCmd1)

		retCmd2, ok := cm.GetCommand("help")
		assert.True(t, ok)
		assert.Equal(t, mockCmd2, retCmd2)

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
		mockCmd1 := test.NewMockCommand(t)
		mockCmd1.On("Name").Return("valid")

		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(),
			Commands:      []commands.Command{nil, mockCmd1, nil},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmd1, ok := cm.GetCommand("valid")
		assert.True(t, ok)
		assert.Equal(t, mockCmd1, retCmd1)

		_, ok = cm.GetCommand("nil") // Ensure nil commands are not somehow registered
		assert.False(t, ok)
	})

	t.Run("DuplicateCommandNames", func(t *testing.T) {
		mockCmd1a := test.NewMockCommand(t)
		mockCmd1a.On("Name").Return("dup")

		mockCmd1b := test.NewMockCommand(t)
		mockCmd1b.On("Name").Return("dup") // CommandManager logs a warning but takes the first one.

		mockCmd2 := test.NewMockCommand(t)
		mockCmd2.On("Name").Return("unique")

		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        zap.NewNop(), // In a real scenario with a test logger, we could check for the warning.
			Commands:      []commands.Command{mockCmd1a, mockCmd1b, mockCmd2},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm)

		retCmdDup, ok := cm.GetCommand("dup")
		assert.True(t, ok)
		assert.Equal(t, mockCmd1a, retCmdDup) // Should be the first one registered
		assert.NotEqual(t, mockCmd1b, retCmdDup)

		retCmdUnique, ok := cm.GetCommand("unique")
		assert.True(t, ok)
		assert.Equal(t, mockCmd2, retCmdUnique)
	})

	t.Run("NilLogger", func(t *testing.T) {
		mockCmd1 := test.NewMockCommand(t)
		mockCmd1.On("Name").Return("testlog")

		params := commands.CommandManagerParams{
			ApplicationID: appID,
			Logger:        nil, // Explicitly nil
			Commands:      []commands.Command{mockCmd1},
		}

		cm := commands.NewCommandManager(params)
		require.NotNil(t, cm) // Should default to zap.NewNop()

		retCmd1, ok := cm.GetCommand("testlog")
		assert.True(t, ok)
		assert.Equal(t, mockCmd1, retCmd1)
	})
}
