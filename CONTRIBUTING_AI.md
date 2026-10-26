# AI Agent Contribution Guide for this Go Project

This document provides mandatory guidelines for AI agents contributing to this codebase. Adhering to these patterns is crucial for maintaining code quality, consistency, and predictability.

## 1. Core Principles

The primary principle is to **leverage existing patterns and libraries before introducing new ones**. The project relies heavily on a shared helper module, `github.com/fmotalleb/go-tools`, for common concerns like logging and context management. Before implementing any new functionality, you must inspect the surrounding code and utilize these established helpers.

## 2. Configuration

Project configuration is managed in a structured and type-safe way. All configuration changes must follow this pattern.

- **Definition**: Configuration structures are defined in `config/config.go`. When adding a new configuration field, you must add it to the appropriate struct in this file.
- **Loading**: Configuration is loaded from a `config.yaml` file into the Go structs using the `config.Parse()` function. You do not need to implement custom loading logic.
- **Validation**: **All new configuration fields must be validated.** Add validation logic to the `Validate` function in `config/validate.go`. Use the `validator` struct tags and custom validation functions within this file to ensure all configuration is correct at startup. A server start-up should fail if the configuration is invalid.

**Example Workflow for adding a new config value `Timeout`:**

1.  Add `Timeout time.Duration` to a struct in `config/config.go`.
2.  Add a validation rule in `config/validate.go`, such as checking that `Timeout` is greater than zero.
3.  Add the new field to `config.yaml.example` with a sensible default.

## 3. Logging

Logging is exclusively handled by **`go.uber.org/zap`** through a wrapper provided by `github.com/fmotalleb/go-tools/log`.

- **Initialization**: Do not initialize your own logger. A project-wide logger is initialized at application start (in `cmd/common.go` via `log.WithNewEnvLogger`) and is passed down via a `context.Context`.
- **Retrieval**: To get the logger in any part of the application, retrieve it from the context:
    ```go
    import "github.com/fmotalleb/go-tools/log"
    
    // ...
    
    func myFunc(ctx context.Context, /* ... */) {
        logger := log.Of(ctx)
        // ...
    }
    ```
- **Usage**: You **must** use structured logging. Provide context as key-value pairs using `zap`'s typed fields. **Never use `fmt.Printf` or the standard `log` package for application logging.**

    **Correct Usage:**
    ```go
    // Log with structured context. The 'zap.Error' field is preferred for errors.
    logger.Warn(
        "Failed to process request",
        zap.String("requestID", "xyz-123"),
        zap.Error(err),
    )
    ```

    **Incorrect Usage:**
    ```go
    // DO NOT do this. This is unstructured and hard to parse.
    logger.Warn("Failed to process request " + "xyz-123" + ": " + err.Error())
    ```

## 4. Error Handling

This project follows modern, idiomatic Go error handling practices.

- **Wrapping**: **Always wrap errors with context** using `fmt.Errorf` and the `%w` verb. This preserves the original error for later inspection. Do not discard the original error.

    **Correct Usage:**
    ```go
    if err != nil {
        return fmt.Errorf("failed to open file %q: %w", filePath, err)
    }
    ```

- **Checking**: To check for a specific error type, use `errors.Is()` for sentinel errors and `errors.As()` for custom error types.

    ```go
    // Check if an error is a specific, known error value.
    if errors.Is(err, context.Canceled) {
        // Handle cancellation gracefully.
    }

    // Check if an error is of a certain type (e.g., a validation error).
    var validationErrs validator.ValidationErrors
    if errors.As(err, &validationErrs) {
        // Handle the validation error specifically.
    }
    ```
- **Creating New Errors**: For simple, static error messages where no underlying error is being wrapped, use `errors.New()`.

    ```go
    if cfg.Listen == "" {
        return errors.New("listen address cannot be empty")
    }
    ```

## 5. Graceful Reloading

The application supports graceful reloading of its configuration and services. This process is managed by the `reloader.WithOsSignal` function from the `go-tools` module and is triggered by OS signals (e.g., `SIGHUP`).

When a reload is triggered, the `reloader` cancels the main application `context.Context` to signal all services to shut down, and then it re-executes the main application logic with the updated configuration.

**Your Responsibility:**

Any long-running service or background goroutine you implement **must** respect its `context`.

- Your function should listen for context cancellation.
- When the context is canceled, your function must stop its work, clean up any resources (e.g., close network connections, stop tickers), and return promptly.

**Correct Usage:**
```go
func myLongRunningService(ctx context.Context) error {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            // Context was canceled, time to clean up and exit.
            logger := log.Of(ctx)
            logger.Info("shutting down myLongRunningService")
            return ctx.Err() // Return the context error.
        case <-ticker.C:
            // Do work...
        }
    }
}
```

Failure to respect the context will prevent graceful reloading, as the old service will not shut down. This will block the new service from starting or lead to a forced, non-graceful termination.
