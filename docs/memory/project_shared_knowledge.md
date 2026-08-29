---
name: shared_knowledge_refactor
description: Future task to create shared knowledge directories for cult organizations instead of duplicating files across agents
type: project
---

Create a shared knowledge repository path (e.g. `data/shared_knowledge/eternal_spark/` and `data/shared_knowledge/grand_architects/`) for both religious organizations. All agents in each group would reference the same single source for shared files (sermons, counter_sermons, sacred_texts, lore, foundational_documents, faction_edit) instead of maintaining copies. Each agent could also contribute additional knowledge that all members can access.

**Why:** Currently shared files (sermons.json, counter_sermons.json, sacred_texts.md, lore.md, foundational_documents.md, faction_edit.json) are duplicated across every agent directory (prophet-1, spark-1 through spark-5, and future grand_architects agents), making it hard to keep them in sync.

**How to apply:** When implementing, update agent loading code to look for shared org-level files in addition to agent-specific files. Each agent keeps only personality.json (and any personal files) in their own directory.
