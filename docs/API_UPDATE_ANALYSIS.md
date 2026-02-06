# SpaceMolt API Update Analysis - Token → Password Migration

## API Change Summary (v0.38.0)

The authentication field was renamed from `token` to `password` for semantic clarity. While functionally unchanged, this affects all authentication code.

**Key Change:**
- Registration now returns a `password` field (64-char hex string) instead of `token`
- Login now expects `password` parameter instead of `token`
- This is a **permanent password** with no recovery mechanism

## Files Requiring Updates

### 1. Core Data Structures

#### pkg/credentials/provider.go
**Current:**
```go
type Credentials struct {
    Username string
    Token    string
    Empire   string
}
```

**Needs to change to:**
```go
type Credentials struct {
    Username string
    Password string  // Changed from Token
    Empire   string
}
```

**Impact:** This is the primary credentials structure used throughout the codebase.

---

#### pkg/game/types.go (Line 147)
**Current:**
```go
type State struct {
    Username string
    Token    string
    // ...
}
```

**Needs to change to:**
```go
type State struct {
    Username string
    Password string  // Changed from Token
    // ...
}
```

**Impact:** State object is used everywhere game state is tracked.

---

### 2. Game Client (pkg/game/client.go)

#### Client struct (Line 21)
**Current:**
```go
type Client struct {
    username string
    token    string
    // ...
}
```

**Needs to change to:**
```go
type Client struct {
    username string
    password string  // Changed from token
    // ...
}
```

#### NewClient function (Line 46)
**Current:**
```go
func NewClient(url, username, token string, debugLogger *log.Logger) *Client
```

**Needs to change to:**
```go
func NewClient(url, username, password string, debugLogger *log.Logger) *Client
```

#### Connect method (Line 86-87)
**Current:**
```go
c.state.Username = c.username
c.state.Token = c.token
```

**Needs to change to:**
```go
c.state.Username = c.username
c.state.Password = c.password
```

#### Login method (Line 136-144)
**Current:**
```go
func (c *Client) Login(ctx context.Context) error {
    if c.token == "" {
        return fmt.Errorf("no token available")
    }

    msg := protocol.Message{
        Type: "login",
        Payload: map[string]any{
            "username": c.username,
            "token":    c.token,  // ← CHANGE THIS
        },
    }
    // ...
}
```

**Needs to change to:**
```go
func (c *Client) Login(ctx context.Context) error {
    if c.password == "" {
        return fmt.Errorf("no password available")
    }

    msg := protocol.Message{
        Type: "login",
        Payload: map[string]any{
            "username": c.username,
            "password": c.password,  // ← Changed from "token"
        },
    }
    // ...
}
```

#### Response handler (Line 343-346)
**Current:**
```go
case protocol.TypeRegistered:
    if token, ok := resp.Payload["token"].(string); ok {
        c.state.Token = token
        c.token = token
    }
```

**Needs to change to:**
```go
case protocol.TypeRegistered:
    if password, ok := resp.Payload["password"].(string); ok {
        c.state.Password = password
        c.password = password
    }
```

---

### 3. Credentials Providers

#### pkg/credentials/file.go
**Current:**
```go
type FileCredentials struct {
    Username string `json:"username"`
    Token    string `json:"token"`
    Empire   string `json:"empire"`
}
```

**Needs to change to:**
```go
type FileCredentials struct {
    Username string `json:"username"`
    Password string `json:"password"`  // Changed from Token
    Empire   string `json:"empire"`
}
```

**Also update conversions:**
```go
// Old
return &credentials.Credentials{
    Username: fc.Username,
    Token:    fc.Token,
    Empire:   fc.Empire,
}, nil

// New
return &credentials.Credentials{
    Username: fc.Username,
    Password: fc.Password,
    Empire:   fc.Empire,
}, nil
```

---

#### pkg/credentials/legacy.go
**Current:**
```go
type LegacyCredentials struct {
    Username string `json:"username"`
    Token    string `json:"token"`
}
```

**Needs to change to:**
```go
type LegacyCredentials struct {
    Username string `json:"username"`
    Password string `json:"password"`  // Changed from Token
}
```

---

#### pkg/credentials/sqlite.go
Update table schema and field references:
- Column name in database
- Field mappings in Insert/Get operations

---

### 4. Agent Manager (pkg/agent/manager.go)

Search for all uses of `creds.Token` and replace with `creds.Password`:

**Line ~250+:**
```go
// Old
gameClient := game.NewClient(m.gameServerURL, username, token, m.debugLogger)

// New
gameClient := game.NewClient(m.gameServerURL, username, password, m.debugLogger)
```

**Line ~394+:**
```go
// Old
if state.Token == "" {
    return fmt.Errorf("no token received after registration")
}

// New
if state.Password == "" {
    return fmt.Errorf("no password received after registration")
}
```

---

### 5. Watcher (cmd/watcher/main.go)

Multiple locations where token is used:

**Line ~129-134:** (credentials loading)
```go
// Old
var username, token string
if data, err := os.ReadFile(credentialsFile); err == nil {
    var creds map[string]string
    if err := json.Unmarshal(data, &creds); err == nil {
        username = creds["username"]
        token = creds["token"]
    }
}

// New
var username, password string
if data, err := os.ReadFile(credentialsFile); err == nil {
    var creds map[string]string
    if err := json.Unmarshal(data, &creds); err == nil {
        username = creds["username"]
        password = creds["password"]
    }
}
```

**Line ~228-229, ~298, ~486, ~526, ~618, ~814-826:** (state initialization and game client creation)
```go
// Old
state := &game.State{
    Username: username,
    Token:    token,
}
client := game.NewClient(url, username, token, debugLogger)

// New
state := &game.State{
    Username: username,
    Password: password,
}
client := game.NewClient(url, username, password, debugLogger)
```

**saveCredentials function (Line ~814):**
```go
// Old
func saveCredentials(username, token string) {
    creds := map[string]string{
        "username": username,
        "token":    token,
    }
    // ...
}

// New
func saveCredentials(username, password string) {
    creds := map[string]string{
        "username": username,
        "password": password,
    }
    // ...
}
```

---

### 6. Command-line Tools

#### cmd/register-agent/main.go
Update credential handling and display

#### cmd/test-agent-manager/main.go
Update test credential creation

#### cmd/test-credentials/main.go
Update credential testing

---

### 7. Test Files

#### pkg/game/client_test.go
#### pkg/game/client_integration_test.go
#### pkg/credentials/provider_test.go
#### pkg/agent/manager_enhanced_test.go

Update all test fixtures and assertions that reference `Token` or `.token`

---

## Migration Strategy

### Option 1: Breaking Change (Clean Migration)
1. Update all structs to use `Password` instead of `Token`
2. Update all JSON tags from `"token"` to `"password"`
3. Users must re-register or manually update their credential files

**Pros:** Clean codebase, matches API semantics
**Cons:** Breaks existing credential files

### Option 2: Backward Compatible (Dual Support)
1. Keep both `Token` and `Password` fields temporarily
2. Support reading both from JSON (with `password` taking precedence)
3. Always write `password` to new files
4. Deprecation period with warnings

**Pros:** Smooth migration for users
**Cons:** Technical debt, more complex code

### Option 3: Automatic Migration
1. Update structs to use `Password`
2. In credential loaders, check for both `"token"` and `"password"` fields
3. If `"token"` found, automatically migrate to `"password"` and save
4. Add migration logging

**Pros:** Transparent to users, clean codebase
**Cons:** More migration code

## Recommended Approach

**Use Option 3 (Automatic Migration)** because:
- Users don't need to manually update files
- Codebase becomes clean and semantically correct
- Migration is transparent and automatic
- One-time migration code is minimal

## Implementation Checklist

- [ ] Update `Credentials` struct (pkg/credentials/provider.go)
- [ ] Update `State` struct (pkg/game/types.go)
- [ ] Update `Client` struct and methods (pkg/game/client.go)
- [ ] Update `FileCredentials` with JSON tag migration support
- [ ] Update `LegacyCredentials` with migration support
- [ ] Update SQLite schema and queries
- [ ] Update agent manager authentication code
- [ ] Update watcher authentication code
- [ ] Update all command-line tools
- [ ] Update all test files
- [ ] Add migration tests
- [ ] Update documentation

## Testing Requirements

1. **Unit Tests:** Verify token→password migration in credential loaders
2. **Integration Tests:** Test full authentication flow with new API
3. **Backward Compat Tests:** Verify old credential files are migrated
4. **Manual Testing:**
   - Register new agent (should receive password)
   - Login with existing credentials (should work with migration)
   - Verify credential files are updated to use password

## Documentation Updates

- Update README with password terminology
- Update INTEGRATION_TESTING.md with new credential format
- Add migration notes to CHANGELOG
- Update any API examples in docs/
