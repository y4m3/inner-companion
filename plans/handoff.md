# Handoff Note

- Updated: 2026-02-16
- Workspace: `/home/headmaster/projects/inner-companion`

## Current Goal
Phase 5: Phase 1 MVP ギャップ全項目実装完了。spec.md Phase 1 DoD の残存ギャップ 8 項目を全て解消。

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

### Phase 3 — Session Persistence + Memory Management
- `internal/protocol/contracts.go` — HistoryMessage/HistoryContentBlock 型、AgentRequest/Response に `json:"-"` フィールド追加
- `internal/session/store.go` — Store interface、SessionMeta、EstimateTokens
- `internal/session/sqlite.go` + test — SQLiteStore（WAL mode、foreign keys、sessions + messages テーブル）
- `internal/session/memory.go` + test — Summarizer interface、MemoryManager（70%閾値フラッシュ、BuildSystemPrompt）、LLMSummarizer
- `internal/agent/agent.go` — History prepend、SystemPrompt override、NewMessages 収集
- `internal/gateway/session_runner.go` + test — SessionRunner デコレータ（AgentRunner 実装）
- `cmd/gateway/main.go` — SESSION_DB 環境変数、SQLiteStore → LLMSummarizer → MemoryManager → SessionRunner 配線

### Phase 4 — YAML定義 / bash分類 / 監査 / 認証 / ReadOnly / WebChat / E2E
- `internal/agentdef/` — YAML エージェント定義ロード・バリデーション
- `internal/classify/` — bash コマンド分類（L1/L2/L3）、複合コマンド対応
- `internal/audit/` — 監査ログ（JSONL FileLogger / NopLogger）
- `internal/gateway/auth.go` — Gateway ペアリング認証ミドルウェア
- `internal/gateway/readonlymode.go` — 読み取り専用モード管理
- `internal/tool/classified_bash.go` — bash 分類 + ポリシー適用
- `web/static/` — WebChat UI（PC最小版）
- `e2e/` — E2E テストスイート

### Phase 5 — Phase 1 MVP ギャップ解消（全 8 項目）

#### 5-1: セッション当たりツール失敗上限 (10回) — §6.1
- `internal/gateway/session_runner.go` — `failCounts map[string]int` で累積ツール失敗数を追跡
- 10 回超過で `"tool failure limit exceeded (10 per session)"` エラー返却
- テスト 3 件追加（10回許可、11回拒否、成功カウント除外）

#### 5-2: リクエスト当たりツール総実行時間 (300s) — §6.1
- `internal/agent/agent.go` — `requestTimeout` フィールド追加（デフォルト 300s）
- `Run()` 冒頭で `context.WithTimeout(ctx, 300s)` 適用
- 個別ツール 120s + 全体 300s の二重制限
- テスト 1 件追加（タイムアウト検証）

#### 5-3: ネットワーク制限 (DialContext allowlist) — §4.2
- `internal/anthropic/transport.go` — `NewRestrictedTransport()` で許可ホスト以外のTCP接続をブロック
- `internal/anthropic/client.go` — `ClientConfig.HTTPClient` フィールド追加
- `cmd/gateway/main.go` — `NETWORK_ALLOWLIST` 環境変数（デフォルト `api.anthropic.com`）
- テスト 3 件追加（許可/拒否/複数ホスト）

#### 5-4: ReadOnly 復旧ヘルスチェック (2回成功) — §6.2
- `internal/gateway/readonlymode.go` — `healthCheck func(ctx context.Context) bool` フィールド、`SetHealthCheck()`、`TryRecover(ctx)` に 2 回連続チェック追加
- `internal/gateway/session_runner.go` — ReadOnly 時に `TryRecover(ctx)` 自動呼び出し
- `cmd/gateway/main.go` — LLM ping ベースのヘルスチェック関数注入
- テスト 4 件追加（2回成功/1回目失敗/2回目失敗/nil スキップ）

#### 5-5: bash 変更検出 + checksum 監査 — §4.3
- `internal/tool/changeset.go` — `DetectChanges()` で git diff + SHA256 checksum 計算
- `internal/tool/classified_bash.go` — bash 実行後に `DetectChanges()` 呼び出し、`bash_changeset` 監査イベント記録
- テスト 6 件追加（変更なし/新規/修正/削除/SHA256長/非git）+ 統合テスト 1 件

#### 5-6: git 自動コミット — §3.1, §3.3
- `internal/tool/autocommit.go` — `AutoCommit()` で `git add -A` → `git commit`
- `internal/tool/classified_bash.go` — `SetAutoCommit(bool)` で有効化、変更検出後に自動コミット
- `cmd/gateway/main.go` — `AUTO_COMMIT` 環境変数（デフォルト `false`）
- テスト 4 件追加（コミット生成/変更なし/非git/無効時）+ 統合テスト 2 件

#### 5-7: cgroups v2 non-root 運用 — §4.5
- `internal/sandbox/cgroup.go` — `CgroupAvailable()` で `systemd-run --user` 検出、`RunInCgroup()` でスコープ実行
- `internal/sandbox/exec.go` — `RunInSandbox()` で cgroup 利用可能時に `systemd-run` ラップ（CPUQuota=100%, MemoryMax=512M）
- フォールバック: systemd-run 不可時は既存 landlock+seccomp+rlimits 維持

#### 5-8: L2 累積閾値 (ファイル数/行数) — §4.4.1
- `internal/tool/classified_bash.go` — `l2FileCount`, `l2LineCount` フィールド追加
- supervised L2 チェック拡張: `l2Count > 20 || l2FileCount >= 10 || l2LineCount >= 500`
- 最初に到達した条件で発火
- テスト 2 件追加（ファイル数超過/行数超過）

## Files

### New (Phase 5)
- `internal/anthropic/transport.go` + test — ネットワーク制限 Transport
- `internal/sandbox/cgroup.go` + test — cgroups v2 non-root 運用
- `internal/tool/changeset.go` + test — bash 変更検出 + checksum
- `internal/tool/autocommit.go` + test — git 自動コミット

### Modified (Phase 5)
- `internal/agent/agent.go` + test — requestTimeout (300s) 追加
- `internal/anthropic/client.go` — HTTPClient フィールド追加
- `internal/gateway/readonlymode.go` + test — healthCheck + TryRecover(ctx)
- `internal/gateway/session_runner.go` + test — tool failure limit + TryRecover 呼び出し
- `internal/tool/classified_bash.go` + test — 変更検出/自動コミット/L2累積閾値統合
- `internal/sandbox/exec.go` — cgroup ラップ追加
- `cmd/gateway/main.go` — 全新機能の配線

## Environment Variables (Phase 5 追加)
| 変数 | 用途 | デフォルト |
|------|------|----------|
| NETWORK_ALLOWLIST | 許可ホスト（カンマ区切り） | api.anthropic.com |
| AUTO_COMMIT | bash 変更後の自動コミット | false |

## Executed Tests
- `go test ./...` — 全 PASS
- `go test -race ./...` — race なし
- `go vet ./...` — クリーン

## Remaining for spec.md Phase 1 MVP
- E2E テスト 10 シナリオ到達（現在のシナリオ数に追加が必要な場合）

## Open Items
- なし
