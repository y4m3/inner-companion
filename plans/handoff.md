# Handoff Note

- Updated: 2026-02-16 09:10:01 JST
- Workspace: `/home/headmaster/projects/inner-companion`
- Git: このディレクトリは Git 管理下ではありません（`.git` なし）

## Current Goal
Phase 1 の Gateway 最小実装を TDD で固める。

## Completed
- `POST /v1/messages` の基本実装
- Inbound/Outbound/Agent 契約バリデーション
- 冪等性（`session_id:agent_id:client_msg_id`）
- inflight 重複要求の集約
- `status=error` 応答はキャッシュしない
- idempotency cache に TTL 導入
- TTL 起算を agent 応答完了時刻に修正
- panic 時の recover と inflight クリーンアップ
- `Shutdown()` 追加（inflight run をキャンセル、以降新規要求は失敗）
- bad request の内部詳細マスク
- flaky になりうる sleep 依存テストをチャネル同期に変更

## Files Touched
- `internal/gateway/service.go`
- `internal/gateway/service_test.go`
- `internal/gateway/http.go`
- `internal/gateway/http_test.go`

## Executed Tests (latest)
- `go test ./...`
- `go test -race ./internal/gateway`

## Open Items
- `Content-Type` 未指定時を 415 にするかは仕様未確定
- TTL 期限切れエントリの定期パージ（現状は上限 eviction + 再アクセス時削除）

## Resume Commands
1. `cd /home/headmaster/projects/inner-companion`
2. `go test ./...`
3. `go test -race ./internal/gateway`
4. `sed -n '1,260p' internal/gateway/service.go`
5. `sed -n '1,360p' internal/gateway/service_test.go`

## Notes
- Claude read-only review は最終時点で `NONE`（指摘なし）。
