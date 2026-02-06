# Prompt Management System

The Prompt Management System provides a template-based approach to managing LLM prompts with versioning, metrics collection, and evolutionary improvements.

## Overview

The system transforms hardcoded prompts into an external, template-based system that supports:

- **Version Control**: Track different versions of prompts
- **Dynamic Personalization**: Templates include full agent personality traits
- **Metrics Collection**: Automatic tracking of prompt performance
- **Safe Evolution**: Draft/review/activate workflow for improvements
- **Fallback Safety**: Multiple layers of error recovery

## Architecture

### Core Components

1. **PromptManager** (`pkg/prompts/manager.go`)
   - Loads and caches Go text templates
   - Renders templates with dynamic context
   - Thread-safe template management

2. **TemplateContext** (`pkg/prompts/context.go`)
   - Data structures for template rendering
   - Includes agent personality, state, knowledge, history
   - Builder functions for context creation

3. **Registry** (`pkg/prompts/registry.go`)
   - Scans and tracks available templates
   - Manages version metadata
   - Distinguishes active/draft/deprecated versions

4. **Selector** (`pkg/prompts/selector.go`)
   - Chooses appropriate template version
   - Role-based overrides
   - Fallback strategies

5. **MetricsCollector** (`pkg/prompts/metrics.go`)
   - Tracks success rates, confidence, errors
   - Per-role metrics
   - YAML-based storage

6. **Evolver** (`pkg/prompts/evolution.go`)
   - LLM-powered prompt analysis
   - Draft version creation
   - Promotion workflow

## Directory Structure

```
data/prompts/
├── templates/
│   ├── decision/
│   │   ├── decision.v1.tmpl
│   │   └── decision.v2-draft.tmpl (optional)
│   ├── feedback/
│   ├── analysis/
│   ├── improvement/
│   └── shared/
│       ├── personality_context.tmpl
│       ├── actions_list.tmpl
│       └── json_format.tmpl
├── config.yaml
└── metadata/
    └── decision.v1.yaml (metrics)
```

## Template Syntax

Templates use Go's `text/template` syntax with custom helper functions.

### Available Data

Templates have access to `TemplateContext`:

```go
type TemplateContext struct {
    AgentID       string
    AgentName     string
    Role          string
    Personality   *PersonalityContext  // Traits, skills, motivations
    State         *StateContext        // Fuel, hull, cargo, location
    Knowledge     *KnowledgeContext    // Known systems, POIs
    History       *HistoryContext      // Recent actions and experiences
    LastFeedback  *FeedbackContext     // Previous action result
    System        *SystemContext       // Available actions, tick
}
```

### Example Template

```go-template
You are {{.AgentName}}, a {{.Role}} in the Spacemolt universe.

{{template "personality_context.tmpl" .Personality}}

CURRENT SITUATION:
Location: {{.State.SystemName}}
Fuel: {{printf "%.0f" .State.Fuel}}/{{printf "%.0f" .State.MaxFuel}} ({{printf "%.1f" .State.FuelPercent}}%)
Hull: {{printf "%.0f" .State.Hull}}/{{printf "%.0f" .State.MaxHull}} ({{printf "%.1f" .State.HullPercent}}%)

{{if .LastFeedback}}
LAST ACTION FEEDBACK:
{{if .LastFeedback.Success}}✓{{else}}✗{{end}} {{.LastFeedback.Message}}
{{end}}

{{template "actions_list.tmpl"}}
{{template "json_format.tmpl"}}

Your decision:
```

### Helper Functions

- `printf`: Formatted printing (e.g., `{{printf "%.2f" .Value}}`)
- `join`: Join string arrays (e.g., `{{join ", " .Items}}`)
- `percent`: Calculate percentage (e.g., `{{percent .Fuel .MaxFuel}}`)
- `successIcon`: Show ✓ or ✗ (e.g., `{{successIcon .Success}}`)
- `default`: Provide default value (e.g., `{{default "N/A" .Field}}`)

## Configuration

Edit `data/prompts/config.yaml`:

```yaml
global:
  selection_strategy: stable    # stable, latest, role_based
  fallback_to_previous: true    # Use fallback on error
  collect_metrics: true         # Enable metrics collection

prompts:
  decision:
    active_version: 1           # Default version
    role_overrides:             # Role-specific versions
      explorer: 2
      miner: 1
      trader: 2
    fallback_version: 1         # Version to use if active fails

metrics:
  enabled: true
  retention_days: 90
  storage_path: data/prompts/metadata
```

## Usage

### In Code

The system is automatically integrated into the LLM client:

```go
// LLM client initialization
client, err := llm.New(llm.Config{
    BaseURL: "http://localhost:11434",
    Model: "llama3.2",
    PromptsDir: "data/prompts/templates",
    PromptsConfig: "data/prompts/config.yaml",
})

// Agent automatically uses templates
agent := agent.NewBaseAgent(id, personality, memory, client)
decision, err := agent.Decide(ctx, state)
```

### Fallback Behavior

If the prompt system is unavailable:
1. Warning is logged
2. System continues with hardcoded prompts
3. No errors are raised

This ensures the system is resilient to configuration issues.

## Creating New Templates

### 1. Create Template File

```bash
# Create new version
cat > data/prompts/templates/decision/decision.v2.tmpl <<EOF
You are {{.AgentName}}, a {{.Role}}.

[Your improved prompt here]

{{template "actions_list.tmpl"}}
{{template "json_format.tmpl"}}
EOF
```

### 2. Update Configuration

```yaml
prompts:
  decision:
    active_version: 2  # Change to new version
    fallback_version: 1
```

### 3. Reload

The server will use the new template on next startup. For live reload:

```go
manager.ReloadTemplates()
```

## Metrics

### Viewing Metrics

Metrics are stored in `data/prompts/metadata/`:

```bash
cat data/prompts/metadata/decision.v1.yaml
```

Output:
```yaml
prompt_type: decision
version: 1
total_uses: 150
success_count: 142
error_count: 8
avg_confidence: 0.87
last_used: 2026-02-03T20:00:00Z
by_role:
  miner:
    uses: 50
    success_count: 48
    avg_confidence: 0.89
  explorer:
    uses: 100
    success_count: 94
    avg_confidence: 0.86
```

### Success Metrics

- **Success Rate**: `success_count / total_uses`
- **Error Rate**: `error_count / total_uses`
- **Average Confidence**: Running average of LLM confidence scores

## Evolution Workflow

### 1. Analyze Current Performance

```go
evolver := prompts.NewEvolver(manager, registry, metrics, llmClient)
suggestion, err := evolver.AnalyzePrompt("decision", 1)
```

### 2. Create Draft Version

```go
newContent := `[Improved template content]`
err = evolver.CreateDraftVersion("decision", 1, newContent)
```

This creates `decision.v2-draft.tmpl`.

### 3. Test Draft

Manually update config to use draft:

```yaml
prompts:
  decision:
    active_version: 2-draft  # Test the draft
```

### 4. Promote to Active

```go
err = evolver.PromoteDraft("decision", 2)
```

Renames `decision.v2-draft.tmpl` → `decision.v2.tmpl`

### 5. Activate

Update config:

```yaml
prompts:
  decision:
    active_version: 2
    fallback_version: 1
```

## Best Practices

1. **Always Keep Fallback**: Set `fallback_version` to a known-good version

2. **Test Drafts First**: Never promote directly to active without testing

3. **Monitor Metrics**: Check success rates before and after template changes

4. **Version Immutability**: Once activated, never modify an existing version file

5. **Shared Templates**: Use shared templates for common sections:
   - `personality_context.tmpl`
   - `actions_list.tmpl`
   - `json_format.tmpl`

6. **Role Overrides**: Use role-specific versions only when necessary

7. **Error Handling**: Templates should be forgiving of missing data

## Troubleshooting

### Template Not Found

**Problem**: `template not found: decision.v2`

**Solution**:
- Check file exists: `data/prompts/templates/decision/decision.v2.tmpl`
- Check filename format: `{name}.v{number}.tmpl`
- Reload templates: `manager.ReloadTemplates()`

### Template Parse Error

**Problem**: `failed to parse template: ...`

**Solution**:
- Validate template syntax
- Check for unclosed `{{` or missing `}}`
- Test with simple template first

### Render Error

**Problem**: `failed to render template: ...`

**Solution**:
- Check referenced fields exist in context
- Use `{{default}}` for optional fields
- Add nil checks: `{{if .Field}}...{{end}}`

### Fallback Always Used

**Problem**: System always uses fallback prompt

**Solution**:
- Check `HasPromptManager()` returns true
- Verify template and config paths exist
- Check logs for initialization warnings

## Advanced Topics

### Custom Helper Functions

Add functions to template's `FuncMap`:

```go
funcMap := template.FuncMap{
    "myHelper": func(arg string) string {
        return strings.ToUpper(arg)
    },
}
```

### A/B Testing

Use role overrides to test different versions:

```yaml
prompts:
  decision:
    active_version: 1
    role_overrides:
      explorer: 2  # Explorers use v2
      miner: 1     # Miners use v1
```

### Metrics Analysis

Query metrics programmatically:

```go
m := metrics.GetMetrics("decision", 1)
if m.ErrorRate() > 0.1 {
    log.Warn("High error rate detected")
}
```

## Migration from Hardcoded Prompts

The system maintains backward compatibility:

1. **Old Code**: `llm.BuildDecisionPrompt(...)` still works
2. **New Code**: Templates used automatically when available
3. **Fallback**: If templates fail, falls back to hardcoded prompts

No code changes required for existing agents!

## Future Enhancements

Planned features:

- [ ] CLI tool for template management
- [ ] Web UI for metrics visualization
- [ ] Automatic A/B testing
- [ ] LLM-driven prompt optimization
- [ ] Multi-language prompt support
- [ ] Template validation on commit
- [ ] Performance benchmarking
