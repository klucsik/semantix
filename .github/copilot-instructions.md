# Copilot Instructions for Semantix

## Commit Rules

### 1. Never Commit Secrets
- **NEVER** commit API tokens, passwords, credentials, or keys
- Always use environment variables (e.g., `${DT_API_TOKEN}`, `${DT_ENDPOINT}`)
- Before committing, verify these patterns are NOT staged:
  - `.env`, `.env.*`
  - `*.key`, `*.pem`
  - `credentials.*`, `secrets.*`
  - Any file containing `Api-Token`, `Bearer`, or base64-encoded tokens
- If a potential secret is detected, **WARN the user and DO NOT commit**

### 2. Git Author Identity
- Always commit as: **mreider@gmail.com** / **Matt Reider**
- **NEVER** commit as Claude, Copilot, OpenCode, or any AI identity
- **NEVER** add AI as a co-author (no `Co-authored-by` with AI names)
- When committing, use:
  ```bash
  git -c user.email="mreider@gmail.com" -c user.name="Matt Reider" commit -m "message"
  ```

### 3. README Updates
Always update `README.md` when changes impact:
- Installation or setup instructions
- Configuration options or YAML schema
- CLI usage, flags, or commands
- New features or capabilities
- Breaking changes
- Dependencies or requirements
- Deployment instructions

## Code Style

### Go
- Follow standard `gofmt` formatting
- Use meaningful variable names
- Comments explain "why", not "what"
- Error messages should be actionable

### YAML
- 2-space indentation
- Use comments to explain non-obvious settings
- Environment variables use `${VAR}` or `${VAR:-default}` syntax

### Documentation
- Keep README.md concise but complete
- Include working examples
- Document all environment variables
