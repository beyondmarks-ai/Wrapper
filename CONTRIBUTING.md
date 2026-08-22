# Contributing to Wrapper

Thank you for helping improve Wrapper.

## Development setup

1. Install Go 1.26 or newer.
2. Fork and clone the repository.
3. Create a focused branch from `main`.
4. Make your change with tests when appropriate.
5. Run the checks below before opening a pull request.

```powershell
go fmt ./...
go test ./...
go build -o bin/wrap.exe .
```

On Windows, `scripts/build.ps1` performs the test and build steps for you.

## Pull requests

- Keep each pull request focused on one change.
- Explain the problem and the behavior after your change.
- Include terminal screenshots for visible UI changes.
- Do not commit generated binaries, local configuration, logs, or credentials.
- Use clear commit messages.

## Bug reports

Include your operating system, Wrapper version, terminal, reproduction steps, expected result, actual result, and relevant logs. Use `wrap path-list` to locate the log file.

## Everything integration

Everything-specific changes should be tested on Windows with Everything running and the matching SDK DLL beside `wrap.exe`. Tests must still pass when Everything is unavailable.

## License

By contributing, you agree that your contribution may be distributed under this repository's MIT License.