# Plan: M3-T9 — OpenRouter client + classification

### Task 1: Define OpenRouter API types, errors, and prompts

- [x] Create `prompts/classify_signal.txt` with the classification prompt template (section 41 from spec). Content:
  - System: "You are a signal classifier. Analyze the following public post and determine if it describes a recurring user problem that could be solved by a product. Return only valid JSON."
  - Template fields: `{{.Title}}`, `{{.Body}}`, `{{.Comments}}`, `{{.Source}}`, `{{.URL}}`
  - Decision criteria: is_problem_signal, relevance (0-1), problem description, target_user, context, current_workaround, desired_outcome
  - Classification flags: recurring, product_solvable, is_temporary_incident, is_support_question, is_existing_bug, is_configuration_issue, is_feature_request
  - Hints (0-10): severity_hint, frequency_hint, payment_hint, frustration_hint
  - Keywords, entities, actions, constraints arrays
  - Instruction: "Return valid JSON matching the schema. Do not invent facts."
- [x] Create `internal/openrouter/errors.go` with typed errors: ErrNoAPIKey, ErrNoModel, ErrRateLimited, ErrInvalidResponse, ErrRepairFailed, ErrAllModelsFailed
- [x] Create `internal/openrouter/request.go` with OpenAI-compatible request types: ChatCompletionRequest, Message, ResponseFormat
- [x] Create `internal/openrouter/response.go` with response types: ChatCompletionResponse, Choice, Usage, APIError
- [x] Use `config.OpenRouterConfig` directly; preserve model strings including `:free` suffix

### Task 2: Implement OpenRouter client with retry, fallback, validation, and repair

- [x] Create `internal/openrouter/client.go`:
  - `type Client struct` with `*http.Client`, `config.OpenRouterConfig`, `apiKey string`, `stats Stats`
  - `func New(cfg config.OpenRouterConfig, apiKey string) (*Client, error)` — validates apiKey non-empty
  - `func (c *Client) Complete(ctx context.Context, req domain.CompletionRequest) (domain.CompletionResponse, error)` — implements `domain.LLMClient`
  - POST to `{BaseURL}/chat/completions` with `Authorization: Bearer <key>`, `Content-Type: application/json`
  - Build messages from `req.System` (system role) and `req.Prompt` (user role)
  - Model resolution order: `req.Model` → `cfg.Model` → `cfg.FallbackModels[0]` → ... → `cfg.FallbackModels[N]`
  - Remove empty model strings and duplicates
  - Never include API key in logs or errors
- [x] Create `internal/openrouter/fallback.go`:
  - `func (c *Client) tryModel(ctx, model, system, prompt string, schema any) (CompletionResponse, error)` 
  - Retry loop: exponential backoff with jitter, max 30s, capped at `cfg.MaxRetries`
  - 429 handling: read `Retry-After` header (numeric seconds or HTTP-date), wait that duration
  - 5xx / transport errors: retry with backoff
  - Non-429 4xx: fail immediately, try next fallback model
  - After all models exhausted: return `ErrAllModelsFailed`
- [x] Create `internal/openrouter/validation.go`:
  - `func (c *Client) validateResponse(content string, schema any) ([]byte, error)` 
  - Check content is valid JSON via `json.Valid` or `json.Unmarshal`
  - When `schema != nil`: validate required fields present, ranges in bounds (0-10 for hints, 0-1 for relevance)
- [x] Create `internal/openrouter/repair.go`:
  - `func (c *Client) repairJSON(ctx, model, invalidContent string) ([]byte, error)`
  - Send repair request with `cfg.RepairTemp` (0), same model, including invalid content + schema instructions
  - Validate repaired output once
  - If repair fails or output invalid: return `ErrRepairFailed`, do NOT retry repair
- [x] Expose `func (c *Client) Stats() Stats` with Attempts count for CLI reporting

### Task 3: OpenRouter client tests with fake HTTP server

- [x] Create `internal/openrouter/client_test.go` with `httptest.NewServer`:
  - Test successful request/response: verify auth header, URL, model name, content, usage mapping
  - Test `:free` model suffix preserved in request
  - Test 429 retry with `Retry-After: 5` (inject fake clock to avoid real wait)
  - Test 5xx retry exhaustion → fallback to next model
  - Test all models fail → `errors.Is(err, ErrAllModelsFailed)`
  - Test timeout via context cancellation
  - Test malformed JSON response → validation error
  - Test repair: valid JSON passes, malformed JSON gets repaired once, second malformed fails
  - Test API key not leaked in any error message
  - Test `CompletionRequest.Schema` validation: valid/hint ranges/high values

### Task 4: Implement classification engine

- [x] Create `internal/classify/classify.go`:
  - `type Config struct { Model string; BatchSize int; Temperature float64; MaxTokens int; PromptPath string }`
  - `type Classifier struct { client domain.LLMClient; cfg Config }`
  - `func New(client domain.LLMClient, cfg Config) *Classifier`
  - `func (c *Classifier) Classify(ctx context.Context, raw []domain.RawSignal) ([]domain.ProblemSignal, []ClassifyFailure)`
  - `type ClassifyFailure struct { RawSignalID string; Err error }`
  - Load prompt template from `cfg.PromptPath` via `os.ReadFile` + `text/template`
  - Process signals in batches of `cfg.BatchSize` (default 20 from config.ClassificationBatchSize)
  - For each batch: render template per signal → submit one `domain.CompletionRequest` per signal
  - Parse LLM JSON response into ProblemSignal fields
  - Set `ID = storage.GenerateID("ps")`, `RawSignalID`, `Source`, `URL`, `ClassificationModel`, `ClassifiedAt = time.Now()`
  - Non-problem signals: `IsProblemSignal: false` (still saved so not re-classified)
  - Partial failure: log error, continue to next signal, return failures list
- [x] Create `internal/classify/classify_test.go`:
  - Test prompt rendering with fake raw signal
  - Test full field mapping (valid JSON → ProblemSignal)
  - Test noise mapping (is_problem_signal=false)
  - Test malformed LLM JSON → error in failures list
  - Test partial failure: 3 signals, 1 fails → 2 success + 1 failure
  - Test batch processing with fake LLMClient

### Task 5: Implement CLI classify command

- [x] Create `internal/cli/classify.go`:
  - `ClassifyCmd` with `Use: "classify"`, `Short: "Classify raw signals using LLM"`
  - Flags: `--limit` (0 = all), `--batch-size` (default from config), `--model` (override), `--resume`, `--force`, `--dry-run`
  - `runClassify(cmd, args)`:
    1. Init signalforge dir, load config, load memory, init storage
    2. List unclassified raw signals via `store.ListFiles("raw-signals", ".json")` 
    3. Filter: skip signals that have a matching file in `problem-signals/` (unless --force)
    4. Apply `--limit` if > 0
    5. If `--dry-run`: print count, batch size, model, exit
    6. Create `openrouter.Client` from config + `OPENROUTER_API_KEY`
    7. Create `classify.Classifier` with config, prompt path
    8. Call `classifier.Classify(ctx, signals)`
    9. Save each ProblemSignal via `store.SaveJSON("problem-signals/<id>.json", ps)`
    10. Update memory stats (LLMRequests, ProblemSignalsFound, NoiseSignals)
    11. Save memory atomically
    12. Print summary: N classified, M failures, K noise
- [x] Add CLI tests: use temp `SIGNALFORGE_HOME`, inject fake client, test dry-run, force, limit, partial failures

### Task 6: Wire command, push, PR, and verify

- [x] Modify `cmd/signalforge/main.go` — add `cli.ClassifyCmd` to `init()` alongside existing commands
- [ ] Create GitHub issue for M3-T9 from kanban task
- [ ] Verify: `signalforge classify --help` shows all flags
- [ ] Push branch: `git push -u origin HEAD`
- [ ] Create PR: `gh pr create --title "feat: M3-T9 OpenRouter client + classification" --body "Closes #<issue>" --label enhancement`
- [ ] Run `go build ./...` and `go vet ./...`
- [ ] Run `go test ./...` — all tests pass
- [ ] Post-review: apply any fixes via codex CLI, not patch
- [ ] Check merge conflicts: `gh pr view --json mergeable`
- [ ] Merge: `gh pr merge --squash --delete-branch`
- [ ] Close GitHub issue: `gh issue close <N>`
- [ ] Complete kanban task: `hermes kanban complete t_c2508536`

## Validation Commands

```bash
go build ./...
go vet ./...
go test ./...
go vet ./internal/openrouter/...
go vet ./internal/classify/...
```