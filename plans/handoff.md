# Handoff Note

- Updated: 2026-02-16
- Workspace: `/home/headmaster/projects/inner-companion`

## Current Goal
Phase 2: Anthropic Agent + Tools + Sandbox 実装完了。

## Completed

### Phase 1 — Gateway
- `POST /v1/messages` の基本実装
- Inbound/Outbound/Agent 契約バリデーション
- 冪等性（`session_id:agent_id:client_msg_id`）
- inflight 重複要求の集約
- `status=error` 応答はキャッシュしない
- idempotency cache に TTL 導入
- panic recover + Shutdown()
- TTL 期限切れエントリの定期パージ

### Phase 2 — Anthropic Agent + Tools + Sandbox
- `internal/anthropic/` — Anthropic Messages API クライアント（LLMClient interface、RetryClient）
- `internal/tool/` — ツール実行フレームワーク（Executor interface、Registry、ReadTool、BashTool）
- `internal/sandbox/` — Linux サンドボックス（Landlock FS 制限、seccomp BPF、rlimits、self-exec パターン）
- `internal/agent/agent.go` — AnthropicRunner（agentic tool-use ループ、max 20 iterations）
- `cmd/gateway/main.go` — AnthropicRunner 配線、sandbox child ハンドリング、127.0.0.1 バインド

## Files

### New (Phase 2)
- `internal/anthropic/types.go` — API リクエスト/レスポンス型
- `internal/anthropic/client.go` + test — LLMClient + Client 実装
- `internal/anthropic/retry.go` + test — RetryClient（指数バックオフ max 3 回）
- `internal/tool/tool.go` + test — Executor interface + Registry
- `internal/tool/read.go` + test — ReadTool（max 1MiB）
- `internal/tool/bash.go` + test — BashTool（sandbox/direct exec、120s timeout）
- `internal/sandbox/sandbox.go` — Config + Available()
- `internal/sandbox/landlock.go` + test — Landlock FS 制限
- `internal/sandbox/seccomp.go` + test — seccomp BPF フィルタ
- `internal/sandbox/rlimit.go` — RLIMIT_NPROC/AS/CPU
- `internal/sandbox/exec.go` — RunInSandbox self-exec パターン
- `internal/agent/agent.go` + test — AnthropicRunner

### Modified (Phase 2)
- `cmd/gateway/main.go` — AnthropicRunner 配線
- `go.mod` / `go.sum` — golang.org/x/sys 追加

## Executed Tests
- `go test ./...` — 全 PASS
- `go test -race ./...` — race なし
- `go vet ./...` — クリーン

## Open Items
- なし
