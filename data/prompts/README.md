# Prompts Directory

This directory contains the template-based prompt management system for the Spacemolt Agent Server.

## Structure

```
prompts/
├── templates/         # Prompt template files
│   ├── decision/      # Decision-making prompts
│   ├── feedback/      # Feedback processing prompts
│   ├── analysis/      # Analysis prompts
│   ├── improvement/   # Self-improvement prompts
│   └── shared/        # Shared template components
├── config.yaml        # Configuration file
├── metadata/          # Performance metrics (auto-generated)
└── README.md          # This file
```

## Quick Start

### View Current Templates

```bash
ls templates/decision/
# decision.v1.tmpl
```

### Test Template Rendering

```bash
./agent-server --agents=miner-2
# Check logs for "Prompt management system enabled"
```

### Check Metrics

```bash
cat metadata/decision.v1.yaml
```

## Creating a New Template Version

1. Copy existing template:
   ```bash
   cp templates/decision/decision.v1.tmpl templates/decision/decision.v2-draft.tmpl
   ```

2. Edit the draft template

3. Update config.yaml to test:
   ```yaml
   prompts:
     decision:
       active_version: 2  # Use new version
   ```

4. Test with agents

5. If successful, remove "-draft" suffix:
   ```bash
   mv templates/decision/decision.v2-draft.tmpl templates/decision/decision.v2.tmpl
   ```

## Template Naming Convention

- Active: `{name}.v{number}.tmpl` (e.g., `decision.v1.tmpl`)
- Draft: `{name}.v{number}-draft.tmpl` (e.g., `decision.v2-draft.tmpl`)
- Shared: `{name}.tmpl` (e.g., `personality_context.tmpl`)

## Configuration

Edit `config.yaml` to:

- Set active template versions
- Configure role-specific overrides
- Enable/disable metrics collection
- Set fallback versions

See [PROMPT_SYSTEM.md](../../docs/PROMPT_SYSTEM.md) for full documentation.

## Safety Rules

1. **Never delete active templates** - agents may still reference them
2. **Always set fallback_version** - ensures system resilience
3. **Test drafts before activation** - prevent production errors
4. **Never modify active templates** - create new versions instead

## Metrics

Metrics are automatically collected in the `metadata/` directory:

- `{prompt}.v{N}.yaml` - Per-version metrics
- Updated on each prompt use
- Includes success rates, confidence scores, errors

## Need Help?

See the full documentation:
- [Prompt System Documentation](../../docs/PROMPT_SYSTEM.md)
- [Template Syntax Guide](../../docs/PROMPT_SYSTEM.md#template-syntax)
- [Troubleshooting Guide](../../docs/PROMPT_SYSTEM.md#troubleshooting)
