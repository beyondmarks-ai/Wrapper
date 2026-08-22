# prompt package
This is for the Prompt modal of wrapper

Handles user input updates, wrap model updates, and returns a PromptAction to model.


# Coverage

```bash
cd /path/to/ui/prompt
# Basic coverage
go test -cover

# HTML report
go test -coverprofile=coverage.out && go tool cover -html=coverage.out -o coverage.html
```
Current coverage is 91.3%.
