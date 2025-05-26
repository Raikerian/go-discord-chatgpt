---
applyTo: '**/*.go'
---
# Golang Best Practices

This document outlines best practices for writing Go code, drawing from Google's Go Style Guide and Effective Go.

## Documentation and API Reference

*   **Go Doc Command**: Use the `go doc <package>` command to access comprehensive API documentation for any Go package, including interfaces, types, functions, and usage examples. This works for both standard library packages and third-party dependencies.
    *   Examples: `go doc fmt`, `go doc context`, `go doc github.com/gin-gonic/gin`
    *   For specific symbols: `go doc package.Symbol` (e.g., `go doc fmt.Printf`)
    *   For detailed documentation: `go doc -all package` to see all exported symbols
*   **Godoc Server**: Run `godoc -http=:6060` locally to browse documentation in a web interface, or use `go doc -http=:6060` in newer Go versions.

## Naming

*   **Package Names**: Use short, concise, all-lowercase names. Avoid underscores or camelCase. The name should be the default for imports. E.g., `package http` not `package http_utils`.
*   **Interface Names**: One-method interfaces are often named by the method name plus an -er suffix (e.g., `Reader`, `Writer`). For multiple methods, choose a name that accurately describes its purpose.
*   **Getters**: Getter methods should be named `Owner()` not `GetOwner()`.
*   **Variable Names**:
    *   Use short variable names for variables with a small scope. `i` is fine in a loop.
    *   Longer names are justified for variables that live longer and are used further from their declaration.
    *   MixedCaps or camelCase is the convention (e.g., `numProcessed`).
    *   Acronyms (e.g., `URL`, `ID`) should be consistently cased. For example, `ServeHTTP`, `userID`, `PlayerID`. If an acronym is unexported, a lowercase version (e.g., `urlProcessor`) might be acceptable if it improves readability over an all-caps version. `ID` is generally preferred over `Id`.
*   **Constants**: Named with MixedCaps or camelCase, just like variables. Do not use `ALL_CAPS`.
*   **Function Names**: Use MixedCaps.

## Formatting

*   **gofmt**: Always use `gofmt` to format your code. This is the standard and ensures consistency.
*   **Line Length**: While `gofmt` handles most formatting, be mindful of overly long lines. Break them up for readability where it makes sense.
*   **Indentation**: Use tabs for indentation. `gofmt` enforces this.

## Comments

*   **Godoc**: Document all exported names (types, functions, constants, variables). Also document unexported types and functions when their purpose is not immediately obvious. Package-level declarations should also be documented.
    *   Comments for exported symbols should start with the name of the symbol.
    *   `// Package <name> ...` for package comments.
    *   Use complete, grammatically correct sentences.
*   **Clarity**: Write comments to explain *why* code is the way it is, not just *what* it does, especially for non-obvious logic.
*   **TODOs**: Use `// TODO:` for things that are temporary, a short-term solution, or good-enough but not perfect. Include your username or a reference to an issue if possible.

## Declarations and Initialization

*   **Short Variable Declarations (`:=`)**: Use `:=` for local variables when the type is obvious from the right-hand side.
*   **`var` for Zero Values**: If a variable is intentionally initialized to its zero value, use `var foo string`.
*   **`var` for Non-Obvious Types**: If the type is not clear from the initializer, use `var` with an explicit type.
*   **Composite Literals**: Use composite literals to initialize structs, arrays, slices, and maps.
    *   `s := MyStruct{Field1: "value1", Field2: 123}`
    *   Always include the field names for struct literals to improve clarity and robustness against field reordering.
*   **`make` vs `new`**:
    *   `new(T)`: Allocates memory for a `T`, initializes it to its zero value, and returns `*T` (a pointer to the zeroed `T`). Use when you need a pointer to a zero-initialized struct or other type.
    *   `make(T, args)`: Only for slices, maps, and channels. It initializes the underlying data structures and returns an initialized (not zeroed) value of type `T` (not `*T`).
*   **`init()` function**: Use `init` for setup tasks that must run before package execution begins (e.g., initializing global variables that depend on state). A package can have multiple `init` functions, even across multiple files in the same package. They are executed in the order they appear to the compiler (after all variable declarations are processed), with `init` functions in imported packages running first.

## Control Structures

*   **`if`**:
    *   Simple `if` statements are preferred.
    *   An `if` statement can include an optional short initialization statement, scoped to the `if`/`else` blocks. E.g., `if err := file.Chmod(0664); err != nil { ... }`.
    *   Avoid unnecessary `else` blocks, especially if the `if` block ends with `return`, `break`, `continue`, or `panic`.
*   **`for`**:
    *   Go has only one looping construct: `for`.
    *   `for initialization; condition; post {}` (C-style)
    *   `for condition {}` (while-style)
    *   `for {}` (infinite loop)
    *   `for key, value := range collection {}`
*   **`switch`**:
    *   Cases do not fall through by default. Use `fallthrough` explicitly if that behavior is needed (rare).
    *   Can switch on values of any comparable type.
    *   Can be used without an expression (tagless switch), equivalent to `switch true {}`. This is useful for writing cleaner if-else-if-else chains.
*   **Type Switch**: A form of switch used to discover the dynamic type of an interface variable. `switch x.(type) { case int: ... case string: ... default: ... }`

## Functions

*   **Multiple Return Values**: Use multiple return values for functions that can "fail" (e.g., return `(value, error)`).
*   **Named Result Parameters**: Can be useful for clarity, especially if the function is complex or has multiple return values of the same type. They are initialized to their zero values when the function begins. A bare `return` statement will return the current values of the named results. Use them judiciously, as they can sometimes make code harder to read if overused.
*   **Variadic Functions**: Use `...T` for functions that accept a variable number of arguments of type `T`.
*   **Defer**: Use `defer` to ensure a function call is executed before the surrounding function returns. Useful for cleanup (e.g., closing files, unlocking mutexes). Deferred calls are executed in Last-In, First-Out (LIFO) order.
*   **Function Argument Lists**:
    *   For functions with many arguments, especially if many are optional or have the same type, consider using a single struct to pass arguments. This can improve readability and make it easier to add/remove arguments later (related to the functional options pattern).
    *   Group arguments by meaning or type if it improves readability.

## Error Handling

*   **Explicit Error Checking**: Check errors explicitly. Do not ignore them.
    ```go
    val, err := someFunc()
    if err != nil {
        // handle error
        return err // or log, or wrap, etc.
    }
    // use val
    ```
*   **`error` Type**: The built-in `error` type is an interface. `errors.New` and `fmt.Errorf` are common ways to create error values.
*   **Error Wrapping**: Use `fmt.Errorf` with the `%w` verb to wrap errors. This allows the original error to be preserved and inspected using `errors.Is` or `errors.As`.
    ```go
    if err != nil {
        return fmt.Errorf("more context about what failed: %w", err)
    }
    ```
*   **Sentinel Errors**: Predefined error values (e.g., `io.EOF`, `sql.ErrNoRows`). Use `errors.Is(err, specificError)` to check for them. Do not compare directly with `==` if the error might be wrapped.
*   **Custom Error Types**: Define custom error types (structs implementing the `error` interface) if you need to convey more structured information than a string. Use `errors.As(err, &customErrType)` to check for and convert to custom error types.
*   **Panic**: Use `panic` for truly exceptional situations that indicate a bug in the program or an unrecoverable state (e.g., an impossible condition being met, out-of-bounds array access). Avoid using `panic` for ordinary error handling where an error return is more appropriate.
    *   **Recover**: `recover` can be used within a deferred function to regain control of a panicking goroutine and convert the panic into an error value. Its use is generally limited to specific scenarios, such as in server handlers to prevent a single client request from crashing the entire server, or to manage package-internal panics.
*   **Logging vs. Returning Errors**: Libraries should generally return errors to the caller, allowing the calling application to decide how to handle them (log, retry, etc.). Applications (e.g., the `main` function or top-level HTTP handlers) are typically responsible for logging errors or displaying them to the user.

## Concurrency

*   **Goroutines**: Use goroutines for concurrent execution. They are lightweight.
    ```go
    go f(x, y, z)
    ```
*   **Channels**: Use channels for communication and synchronization between goroutines. "Share memory by communicating, don't communicate by sharing memory."
    *   `ch := make(chan int)` (unbuffered)
    *   `ch := make(chan int, 10)` (buffered)
    *   Send: `ch <- value`
    *   Receive: `value := <-ch` (blocks until value is available) or `value, ok := <-ch` (non-blocking if `ok` is checked; `ok` is false if channel is closed and empty).
    *   Close channels to signal no more values will be sent: `close(ch)`. Only the sender should close a channel. Attempting to send on a closed channel will panic. Receiving from a closed channel yields the zero value for the channel's type immediately (or `false` for the `ok` variable in the two-value assignment).
*   **`select` Statement**: Use `select` to wait on multiple channel operations simultaneously. It blocks until one of its cases can run, then it executes that case. If multiple are ready, it chooses one at random.
    ```go
    select {
    case v := <-ch1:
        // use v
    case ch2 <- x:
        // x sent
    default:
        // non-blocking operation
    }
    ```
*   **`sync` Package**:
    *   `sync.Mutex` for mutual exclusion to protect shared data accessed by multiple goroutines.
    *   `sync.RWMutex` for read-write locks, allowing multiple readers or one writer.
    *   `sync.WaitGroup` to wait for a collection of goroutines to finish their execution.
    *   `sync.Once` to ensure a piece of code (e.g., initialization) is executed exactly once.
*   **Contexts (`context` package)**: Use `context.Context` for managing cancellation signals, deadlines, timeouts, and request-scoped values across API boundaries and between goroutines.
    *   It's good practice to pass a `Context` as the first argument to functions that may block, perform I/O, or run for a significant duration. Name it `ctx`.
    *   Use `context.WithCancel`, `context.WithDeadline`, `context.WithTimeout` to derive new contexts from a parent context.
    *   Goroutines performing work on behalf of a request should check `ctx.Done()` periodically and terminate gracefully if the context is canceled.

## Packages and Project Structure

*   **Package Cohesion**: Group related types and functions into packages. A package should have a clear purpose.
*   **Avoid Circular Dependencies**: Design your packages to avoid them.
*   **`internal` Directories**: Code in an `internal` directory is only importable by code in the directory tree rooted at the parent of `internal`. This is a way to enforce visibility for package internals.
*   **`pkg` Directories**: Conventionally used for library code that is intended to be used by external applications (i.e., code that you would expect others to import). While a common convention, it's not a special directory recognized by the Go toolchain like `internal`.
*   **Command (`cmd`) Directories**: Main applications (executables) are often placed in a `cmd/<appname>` directory.

## Testing

*   **`_test.go` Files**: Tests reside in files named `*_test.go` in the same package as the code they test (e.g., `foo_test.go` for `foo.go`).
*   **Test Functions**: `func TestXxx(t *testing.T)` where `Xxx` starts with an uppercase letter and describes the test.
*   **Table-Driven Tests**: A common and effective pattern for testing multiple input/output scenarios with the same test logic.
*   **Test Helpers**: Factor out common setup, execution, or assertion logic into helper functions. If a helper function calls `t.Error`, `t.Fatal` or similar methods of `*testing.T`, it should call `t.Helper()` at its beginning to ensure error messages are reported with the correct line number.
*   **Subtests**: Use `t.Run("subtest_name", func(t *testing.T) { ... })` to create subtests. This allows for better organization of complex tests and more granular reporting of test failures.
*   **`testing/iotest`**: Provides utilities for testing I/O, such as `iotest.ErrReader`.
*   **`testing/httptest`**: Provides utilities for HTTP testing, such as `httptest.NewServer` and `httptest.NewRecorder`.
*   **Mocks and Fakes**: Use them judiciously. Prefer testing with real components where feasible and not too complex. When needed, use mocks or fakes to isolate the unit under test from its dependencies or to simulate specific conditions (e.g., network errors).
*   **Assertion Libraries**: While the standard library's `testing` package is sufficient (e.g., `if got != want { t.Errorf(...) }`), external libraries like `testify/assert` or `testify/require` can reduce boilerplate for common assertions. Use `go doc` to explore the API of testing libraries. (This project uses `testify`).
*   **Coverage**: Aim for good test coverage. Use `go test -cover`.

## Miscellaneous

*   **Library Documentation**: When working with third-party libraries, use `go doc <package>` to understand their APIs, interfaces, and usage patterns before implementation.
*   **Avoid Global State**: Minimize reliance on global variables. If necessary, provide them through dependency injection or make them configurable.
*   **String Concatenation**: For building strings iteratively, `strings.Builder` is generally more efficient than repeated `+` or `+=`. `fmt.Sprintf` is fine for simple cases.
*   **Embedding**: Use struct embedding for composition, to "borrow" fields and methods.
*   **Interfaces**:
    *   Define interfaces on the consumer side if only one package consumes the interface and it's not widely applicable. This follows the principle "accept interfaces, return structs."
    *   "Accept interfaces, return structs": Functions should accept interface types as parameters when they need to operate on data that can be represented by different concrete types, offering flexibility to the caller. Functions should generally return concrete struct types, as this provides more information to the caller and makes it easier to evolve the returned type without breaking consumers (consumers can choose to use only the parts of the struct they need).
    *   Small interfaces are preferred (e.g., `io.Reader`, `io.Writer`). Aim for interfaces with only the methods necessary for the task at hand.
*   **Blank Identifier (`_`)**:
    *   Use to ignore values in multiple assignment: `val, _ := funcReturningTwoValues()`
    *   Use for side-effect imports (e.g., registering a database driver or image format): `import _ "image/png"`
    *   To indicate an intentionally unused variable (e.g., a parameter required by an interface implementation but not used in that specific implementation). However, it's often better to remove unused variables if they are not required by an interface or for other specific reasons.

This guide provides a foundation. Always prioritize clarity, simplicity, and readability in your Go code. Adherence to `gofmt` is non-negotiable.