// Code generated by mockery; DO NOT EDIT.
// github.com/vektra/mockery
// template: testify

package test

import (
	"context"

	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/session"
	mock "github.com/stretchr/testify/mock"
)

// NewMockCommand creates a new instance of MockCommand. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockCommand(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockCommand {
	mock := &MockCommand{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}

// MockCommand is an autogenerated mock type for the Command type
type MockCommand struct {
	mock.Mock
}

type MockCommand_Expecter struct {
	mock *mock.Mock
}

func (_m *MockCommand) EXPECT() *MockCommand_Expecter {
	return &MockCommand_Expecter{mock: &_m.Mock}
}

// Description provides a mock function for the type MockCommand
func (_mock *MockCommand) Description() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Description")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockCommand_Description_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Description'
type MockCommand_Description_Call struct {
	*mock.Call
}

// Description is a helper method to define mock.On call
func (_e *MockCommand_Expecter) Description() *MockCommand_Description_Call {
	return &MockCommand_Description_Call{Call: _e.mock.On("Description")}
}

func (_c *MockCommand_Description_Call) Run(run func()) *MockCommand_Description_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCommand_Description_Call) Return(s string) *MockCommand_Description_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockCommand_Description_Call) RunAndReturn(run func() string) *MockCommand_Description_Call {
	_c.Call.Return(run)
	return _c
}

// Execute provides a mock function for the type MockCommand
func (_mock *MockCommand) Execute(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error {
	ret := _mock.Called(ctx, s, e, data)

	if len(ret) == 0 {
		panic("no return value specified for Execute")
	}

	var r0 error
	if returnFunc, ok := ret.Get(0).(func(context.Context, *session.Session, *gateway.InteractionCreateEvent, *discord.CommandInteraction) error); ok {
		r0 = returnFunc(ctx, s, e, data)
	} else {
		r0 = ret.Error(0)
	}
	return r0
}

// MockCommand_Execute_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Execute'
type MockCommand_Execute_Call struct {
	*mock.Call
}

// Execute is a helper method to define mock.On call
//   - ctx
//   - s
//   - e
//   - data
func (_e *MockCommand_Expecter) Execute(ctx interface{}, s interface{}, e interface{}, data interface{}) *MockCommand_Execute_Call {
	return &MockCommand_Execute_Call{Call: _e.mock.On("Execute", ctx, s, e, data)}
}

func (_c *MockCommand_Execute_Call) Run(run func(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction)) *MockCommand_Execute_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(context.Context), args[1].(*session.Session), args[2].(*gateway.InteractionCreateEvent), args[3].(*discord.CommandInteraction))
	})
	return _c
}

func (_c *MockCommand_Execute_Call) Return(err error) *MockCommand_Execute_Call {
	_c.Call.Return(err)
	return _c
}

func (_c *MockCommand_Execute_Call) RunAndReturn(run func(ctx context.Context, s *session.Session, e *gateway.InteractionCreateEvent, data *discord.CommandInteraction) error) *MockCommand_Execute_Call {
	_c.Call.Return(run)
	return _c
}

// Name provides a mock function for the type MockCommand
func (_mock *MockCommand) Name() string {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Name")
	}

	var r0 string
	if returnFunc, ok := ret.Get(0).(func() string); ok {
		r0 = returnFunc()
	} else {
		r0 = ret.Get(0).(string)
	}
	return r0
}

// MockCommand_Name_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Name'
type MockCommand_Name_Call struct {
	*mock.Call
}

// Name is a helper method to define mock.On call
func (_e *MockCommand_Expecter) Name() *MockCommand_Name_Call {
	return &MockCommand_Name_Call{Call: _e.mock.On("Name")}
}

func (_c *MockCommand_Name_Call) Run(run func()) *MockCommand_Name_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCommand_Name_Call) Return(s string) *MockCommand_Name_Call {
	_c.Call.Return(s)
	return _c
}

func (_c *MockCommand_Name_Call) RunAndReturn(run func() string) *MockCommand_Name_Call {
	_c.Call.Return(run)
	return _c
}

// Options provides a mock function for the type MockCommand
func (_mock *MockCommand) Options() []discord.CommandOption {
	ret := _mock.Called()

	if len(ret) == 0 {
		panic("no return value specified for Options")
	}

	var r0 []discord.CommandOption
	if returnFunc, ok := ret.Get(0).(func() []discord.CommandOption); ok {
		r0 = returnFunc()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]discord.CommandOption)
		}
	}
	return r0
}

// MockCommand_Options_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Options'
type MockCommand_Options_Call struct {
	*mock.Call
}

// Options is a helper method to define mock.On call
func (_e *MockCommand_Expecter) Options() *MockCommand_Options_Call {
	return &MockCommand_Options_Call{Call: _e.mock.On("Options")}
}

func (_c *MockCommand_Options_Call) Run(run func()) *MockCommand_Options_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run()
	})
	return _c
}

func (_c *MockCommand_Options_Call) Return(commandOptions []discord.CommandOption) *MockCommand_Options_Call {
	_c.Call.Return(commandOptions)
	return _c
}

func (_c *MockCommand_Options_Call) RunAndReturn(run func() []discord.CommandOption) *MockCommand_Options_Call {
	_c.Call.Return(run)
	return _c
}
