# MuninnDB Work Notes

## Local Instance

- **Binary:** `/usr/local/bin/muninn` v0.4.6-alpha
- **Service:** systemd `muninn.service`, runs as user `muninn`, group `agents`
- **Data dir:** `/home/cairn/.muninn/data/`
- **Config:** `/home/cairn/.muninn/muninn.env`
- **Log:** `/home/cairn/.muninn/data/muninn.log`

## Ports (all localhost)

| Port | Protocol | Purpose |
|------|----------|---------|
| 8475 | REST | JSON API |
| 8476 | HTTP | Web UI |
| 8477 | gRPC | Protobuf |
| 8750 | MCP | JSON-RPC (SSE + Streamable HTTP) |

## Fork

- **Origin:** `github.com/isaac-ranger/muninndb` (our fork)
- **Upstream:** `github.com/scrypster/muninndb`
- **Local clone:** `/home/cairn/muninndb-fork` (main branch)
- **Upstream clone:** `/home/cairn/muninndb` (develop branch, v0.4.6-alpha)

## Vaults

| Vault | Engrams |
|-------|---------|
| cairn | 414 |
| cairn-local-test | 310 |
| isaac | 244 |
| pi | 240 |
| codex | 205 |
| builder | 121 |
| antigravity_agent | 5 |
| antigravity-agent | 1 |
| default | 1 |
| ag_test | 0 |
| cairn_restore | 0 |
| huginn-tinker | 0 |

## Embedding Setup

- **Provider:** Ollama (local, GPU/ROCm)
- **Model:** `gte-qwen2-lean` (custom Modelfile variant of `gte-qwen2`)
- **Architecture:** Qwen2, 1.8B params, Q8_0 quantization
- **Embedding dimension:** 1536
- **Context window:** 8192 tokens (`num_ctx` override; base model supports 131072)
- **Difference from `gte-qwen2`:** Only the `num_ctx 8192` parameter
- **Index status:** 1258 / 1540 engrams indexed
- **Batch endpoint:** `/api/embed` (batch mode, max 64 texts per call)

## Enrichment

- **Provider:** Google Gemini 3 Flash Preview via OpenRouter
- **Status:** Configured in `muninn.env` but disabled in UI plugin settings

## Cognitive Workers

All inactive (Hebbian, Temporal, Contradiction, Confidence). Auto-Associations active at write time.

## Plasticity Presets

| Preset | Hops | Features |
|--------|------|----------|
| Default | 2 | Hebbian, Temporal |
| Reference | 3 | No fading |
| Scratchpad | — | Fast fading, no associations |
| Knowledge Graph | 4 | Strong associations, slow fading |

## Recall Modes

| Mode | Description |
|------|-------------|
| Balanced | Engine default (composite scoring) |
| Semantic | Pure vector match |
| Recent | Recency-biased |
| Deep | 4-hop graph traversal |

## Advanced Plasticity Overrides (per-vault)

- BFS Hop Depth (0-8)
- Semantic Weight (0-1)
- FTS Weight (0-1)
- Relevance Floor (0-1)
- Temporal Halflife (days)

## ACTIVATE Pipeline (6-phase recall)

1. Embed + Tokenize (parallel)
2. Parallel Retrieval (BM25 FTS, HNSW vector, temporal pool)
3. Reciprocal Rank Fusion (k: FTS=60, HNSW=40, temporal=120)
4. Hebbian Boost + PAS Transition Boost
5. BFS Association Traversal (0.7 hop penalty, max 500 nodes)
6. Final Scoring: semantic 35%, FTS 25%, temporal 20%, Hebbian 10%, access 5%, recency 5%

## Embedding Code

- **Plugin interface:** `internal/plugin/embed/embed.go` — `Provider` interface (Name, Init, EmbedBatch, MaxBatchSize, Close)
- **Ollama adapter:** `internal/plugin/embed/ollama.go` — batch via `/api/embed`, legacy fallback `/api/embeddings`
- **Valid dimensions (hardcoded):** 384, 768, 1024, 1536
- **Supported providers:** Local (ONNX), Ollama, OpenAI, Voyage, Cohere, Google, Jina, Mistral

## Backup

- **Pre-fork backup:** `/home/cairn/backups/muninn-pre-fork-2026-03-28` (21.4 MB, all 12 vaults)

## Web UI

- **Stack:** Alpine.js + Tailwind CSS + Cytoscape.js (graph viz)
- **Login:** root / password1
- **Views:** Dashboard, Memories, Graph (Memory + Entity), Session, Observability, Settings, Cluster, Logs

---

## Work: Recall Score Analysis

### Threshold Chain

| Layer | Default | Code Location |
|-------|---------|---------------|
| MCP `handleRecall` | 0.5 | `internal/mcp/handlers.go:270` |
| Engine `Activate()` | 0.1 (when MCP sends 0) | `internal/engine/engine.go:1677` |
| `activation.Run()` | 0.05 (hard floor) | `internal/engine/activation/engine.go:362` |
| `deep` mode preset | 0.1 | `internal/auth/` recall mode lookup |

The MCP tool schema documents 0.5 as the default threshold.

### Observed Vector Scores

With `gte-qwen2-lean` (1536-dim, 1.8B params), vector similarity scores for relevant content on the cairn vault:

| Query | Best vector_score | Range of relevant results |
|-------|-------------------|--------------------------|
| "semantic lookup quality" | 0.411 | 0.27–0.41 |
| "Carl preferences for how I work" | 0.389 | 0.21–0.39 |
| "Token Arcade infrastructure AWS" | 0.536 | 0.37–0.54 |
| "Isaac" | — | single-token query, low discrimination |

### ACT-R Scoring Path

`internal/engine/activation/engine.go:1413`

```
contentMatch = 0.6 × vectorScore + 0.4 × tanh(ftsScore)
baseLevel    = ln(n+1) - d × ln(ageDays / (n+1))
raw          = contentMatch × softplus(baseLevel + hebScale×hebbian + hebScale×transition) / denominator
final        = raw × confidence
```

Clamped to [0, 1]. `denominator = 1 + softplus(0) ≈ 1.693`.

Candidates from the decay or time pools that were not also returned by HNSW or FTS have `vectorScore=0` and `ftsScore=0`, producing `contentMatch=0` and therefore `raw=0` through this path.

### Entity Boost (Post-Pipeline)

`internal/engine/engine_entity_boost.go`

Runs after the 6-phase pipeline completes. Takes the top 5 results, collects all named entities linked to those engrams (0x20 forward index), then scans the vault for all engrams mentioning those entities (0x23 reverse index).

- Each matching engram receives `+0.15` per entity-seed overlap
- Engrams not in the result set are added with `score = 0.15`
- Boosts are additive and stack: an engram sharing entities with multiple seeds accumulates `0.15` per match
- No content relevance check is applied during this pass
- No threshold is applied; only `MaxResults` truncation afterward

### Observed Behavior

Query: "Carl preferences for how I work" at threshold=0, limit=10

| Score | vector_score | Concept |
|-------|-------------|---------|
| 1.30 | 0.00 | Token Arcade pytest runs as carl, not cairn |
| 1.15 | 0.27 | Dream insight: Palimpsest drift engine mirrors Muninn |
| 1.15 | 0.00 | GSD red team running overnight against Token Arcade |
| 1.00 | 0.00 | MCP server needs full redesign, not port |
| 1.00 | 0.25 | Arcade status skill completed 2026-03-28 |
| 1.00 | 0.21 | Muninn recall: use deep mode, not find_by_entity |
| 1.00 | 0.00 | Agent Context Loss Problem |
| 1.00 | 0.36 | Dream insight: crossed mandates as collaboration model |
| 1.00 | 0.00 | Carl's argument for agent teams over subagents |
| 1.00 | 0.00 | tokenarcade-systemd-unit |

Results with `vector_score=0` reached the result set through entity boost, not through semantic or FTS matching. Their scores derive entirely from entity co-occurrence with seed results.

### Embedding Text

- **Write time:** `eng.Concept + " " + eng.Content` — concatenated, sent to Ollama as one string per engram (`internal/plugin/retroactive.go:399`)
- **Query time:** `strings.Join(req.Context, " ")` used for FTS; `req.Context` array passed to embedder directly (`internal/engine/activation/engine.go:459,475`)
- Single context string is the common case via MCP

### Candidate Pool Sizes

`CalcCandidatesPerIndex(vaultSize)` determines k for all three retrieval pools:
- vaultSize ≤ 1000: `k = vaultSize` (returns all engrams)
- vaultSize > 1000: `k = sqrt(vaultSize)`, clamped to [30, 200]

For cairn vault (414 engrams): `k = 414`. All engrams are candidates in every pool.

### Decay Pool

`RecentActive` (`internal/storage/query.go:22`) scans the relevance bucket index (0x10 keyspace), ordered from highest stored relevance downward. Returns top-k by stored relevance, not by query similarity. With k=414 on a 414-engram vault, returns the entire vault.

### RRF Fusion (Phase 3)

Candidates from each pool get `1/(k_constant + rank)` added to their `rrfScore`:
- FTS: k=60
- HNSW: k=40
- Decay: k=120
- Time: k=100
- Transition (PAS): k=50

A candidate appearing in the decay pool only (no FTS or HNSW match) gets a small rrfScore but `vectorScore=0` and `ftsScore=0`.

### Score Flow Through ACT-R

For candidates with `vectorScore=0` and `ftsScore=0`:
- `contentMatch = 0.6×0 + 0.4×tanh(0) = 0`
- `raw = 0 × contextualPrior / denominator = 0`
- `final = 0 × confidence = 0`

These candidates score 0 through ACT-R and are filtered at any threshold > 0.

### Entity Boost Score Path

Entity boost (`internal/engine/engine_entity_boost.go`) runs after the 6-phase pipeline. It can add new engrams to the result set with `score = entityBoostFactor (0.15)` and add `+0.15` per entity-seed overlap to existing results. This is the path by which zero-vector-score engrams appear in results with scores > 0.

Multiple entity overlaps with multiple seeds stack additively. An engram sharing 2 entities with each of 5 seeds accumulates `0.15 × 10 = 1.5`.

### Two Interacting Factors

1. **Default threshold (0.5)** filters out most semantic matches from the embedding model, which produces vector scores in the 0.2–0.5 range for relevant content.

2. **Entity boost** adds results after the pipeline with no content relevance gate. These results can outscore semantically matched results through stacked entity overlap.

---

## Changes Made (2026-03-28)

### 1. Default MCP recall threshold: 0.5 → 0.20

- `internal/mcp/handlers.go:270` — `threshold := float32(0.5)` → `float32(0.2)`
- `internal/mcp/tools.go:157` — tool schema description updated

### 2. Semantic mode threshold: 0.3 → 0.15

- `internal/auth/recall_modes.go` — semantic preset `Threshold: 0.3` → `0.15`
- `internal/mcp/tools.go:166` — mode description updated

### 3. Entity boost gated behind pipeline score

- `internal/engine/engine_entity_boost.go` — rewritten
- New engrams are no longer added to the result set by entity boost alone
- Only engrams already in results with `Score > 0` from the activation pipeline receive boost
- Rationale: entity co-occurrence alone does not indicate query relevance

### 4. Entity boost capped at 0.30 per engram

- `internal/engine/engine_entity_boost.go` — `entityBoostCap = 0.30` (2× entityBoostFactor)
- Per-engram accumulator tracks and enforces the cap
- Previously unbounded: N entities × 5 seeds × 0.15 could exceed 1.0

### Threshold Summary (after changes)

| Mode | Threshold | MaxHops | Notes |
|------|-----------|---------|-------|
| balanced (default, no mode) | 0.20 | preset (2) | was 0.5 |
| semantic | 0.15 | 0 | was 0.3, ACT-R disabled |
| recent | 0.20 | 1 | unchanged |
| deep | 0.10 | 4 | unchanged |
