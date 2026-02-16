# Inner Companion 仕様書（実装時の正本）

## 0. 位置づけ

- このファイルは実装判断の **Source of Truth** とする。
- 調査経緯・外部レビュー履歴は `plans/idea.md` を参照する。
- `plans/idea.md` と矛盾した場合は、本仕様書を優先する。

## 1. 目的と原則

### 1.1 目的

OpenClaw の知見を踏まえ、ローカルファーストAIアシスタントを実用最小で構築する。

### 1.2 アーキテクチャ原則

- ホスト直接実行（DinD回避）
- OS優先順位: Linux（Tier 1）→ macOS（Tier 2）→ Windows（Tier 3）
- Must-Have と Phase 1 MVP を分離して管理する

## 2. 体験仕様（UX）

### 2.1 基本体験

- ユーザーは主にオーガナイザーAIと対話する
- 必要時は個別エージェントに直接指示可能
- セッション検索・再開、通知、履歴参照を提供

### 2.2 UI要件

- WebChat UI（PC最小版をPhase 1）
- レスポンシブ（PC + iPhone）とPWAはPhase 2
- オーガナイザーをホーム画面として扱う

## 3. 機能仕様（優先度）

### 3.1 Must-Have（最終的に必須）

- Gateway（WebSocket + HTTP）
- エージェント定義（YAML）
- 永続メモリ（基本）
- OS別サンドボックス
- Gatewayペアリング認証
- git自動コミット
- マルチモデル対応
- 監査ログ
- ツール（read/write/edit/bash）

### 3.2 Phase 1 MVP（最初に必須）

- Linux only
- 単一セッション + 単一エージェント
- ツール: read + bash
- 単一プロバイダ（Anthropic）
- WebChat UI（PC最小）
- YAMLエージェント定義（最小）
- SQLite永続化（基本）
- 積極的フラッシュ + セッション終了時要約

### 3.3 Phase 1運用前提

- Phase 1の `git自動コミット` は **bashツール経由の変更** を対象とする。
- write/edit は Phase 2 で導入する。

## 4. セキュリティ仕様

### 4.1 防御レイヤ

- Phase 1必須: L1-L5（ネットワーク、認証、ファイル制御、syscall制御、リソース制限）
- Phase 2追加: L6-L7（Capability制御、監査）
- Phase 3追加: L8-L9（復旧、高リスク分離）

### 4.2 Linuxネットワーク制限（Phase 1固定方針）

- Landlock TCP制御が使えないカーネルでは、`net/http Transport.DialContext` 制限を唯一の強制手段として採用。
- `network_allowlist` のホスト制限をこの方式で実施。
- iptables/nftables依存は採用しない（root不要方針維持）。

### 4.3 bash整合性要件

- bashはGatewayが起動するサンドボックス内で実行。
- bash実行後、Gatewayが変更ファイルを検出しチェックサムを監査ログへ記録。
- 直接FS変更とGateway監査を両立する。

### 4.4 承認ポリシー（autonomy / tools / 影響度）

| 操作 | 影響度 | readonly | supervised | full |
|------|--------|----------|------------|------|
| `read` | L1 | 許可 | 許可 | 許可 |
| `bash`（読み取り系） | L1 | 禁止 | 許可 | 許可 |
| `write`, `edit` | L2 | 禁止 | 条件付き許可 | 許可 |
| `bash`（可逆変更） | L2 | 禁止 | 条件付き許可 | 許可 |
| `bash`（不可逆変更） | L3 | 禁止 | 人間承認必須 | 人間承認必須 |

- `full` でもL3は人間承認必須。
- `supervised` のL2は累積閾値超過で承認必須。

#### 4.4.1 L2 累積閾値（数値確定）

`supervised` でのL2は、同一セッション内で次のいずれかを満たした時点で人間承認を必須とする。

- 変更ファイル数: 10ファイル以上
- 追記/削除行数: 合計 500行以上
- L2分類のbashコマンド実行回数: 20回以上

注記:
- 閾値判定は「最初に到達した条件」で発火する。
- 承認後は同セッションでL2操作を継続可能（再承認不要）。
- L3は閾値に関係なく毎回承認必須。

#### 4.4.2 bash分類ルール（判定順序固定）

bashコマンドは以下の順序で分類する。

1. L3パターンに一致したらL3  
2. L2パターンに一致したらL2  
3. 上記以外はL1

Phase 1の初期パターン:

- L1（読み取り）例: `ls`, `cat`, `head`, `tail`, `find`, `rg`, `git status`, `git diff --name-only`
- L2（可逆変更）例: `git checkout -b`, `git add`, `go test`, `npm install`, `mkdir`, `cp`（ワークスペース内）
- L3（不可逆変更）例: `rm -rf`, `git push`, `git reset --hard`, `chmod -R`, 外部公開系操作

補助ルール:

- 未知コマンドはデフォルトでL2扱い（fail-closed）。
- パイプ/複合コマンドは最も高い影響度（L3 > L2 > L1）を採用。
- 判定はGateway側で実施し、判定結果を監査ログへ記録する。

### 4.5 cgroups v2 non-root運用

- `systemd --user` delegationを利用して制限値を適用する。
- delegation不可時はフォールバック（実行時間制限・同時実行制御・プロセス監視）を適用。
- `io.max` 無効時は明示ログを出す。

## 5. コンテキスト/メモリ仕様

### 5.1 3層モデル

- Layer 1: システムプロンプト
- Layer 2: メモリ要約
- Layer 3: 会話履歴

### 5.2 70%積極的フラッシュ責務

- `internal/session`: 閾値監視とイベント発火
- `internal/agent`: LLMへのフラッシュ指示
- `internal/memory`: SQLite + MEMORY.md 永続化
- `internal/gateway`: 監査ログ記録

## 6. 運用仕様

### 6.1 エラーハンドリング

- LLM API: 最大3回指数バックオフ
- ツール: 120s timeout
- 冪等操作のみ最大2回再試行（初期1s指数バックオフ）
- 1リクエストあたりツール総実行時間上限: 300s
- 1セッションあたりツール失敗上限: 10回

### 6.2 読み取り専用モード

- 許可: セッション閲覧、メモリ検索、`read`、ログ参照
- 禁止: 新規エージェント起動、`write`/`edit`/`bash`、外部Webhook実行
- 移行: LLM API失敗3回、または401/403連続2回
- 復帰: 5分クールダウン後、ヘルスチェック2回成功（手動復帰も可）
- 遷移は監査ログに理由付きで記録

### 6.3 JSONL互換ポリシー

- additive-only（新規追加のみ）
- 削除は2バージョン以上のdeprecated期間後
- 破壊的型変更禁止（新フィールド追加で対応）
- 必須化時はデフォルト補完を提供
- N/N-1/N-2 fixtureでCIデコード検証

### 6.4 Phase 1 I/O契約（JSONスキーマ）

#### 6.4.1 WebChat -> Gateway（受信）

```json
{
  "type": "user_message",
  "session_id": "string",
  "agent_id": "string",
  "text": "string",
  "client_msg_id": "string"
}
```

- 必須: `type`, `session_id`, `agent_id`, `text`, `client_msg_id`
- `type` は Phase 1 では `user_message` のみ許可

#### 6.4.2 Gateway -> WebChat（送信）

```json
{
  "type": "assistant_message",
  "session_id": "string",
  "agent_id": "string",
  "text": "string",
  "server_msg_id": "string",
  "in_reply_to": "string"
}
```

```json
{
  "type": "tool_result",
  "session_id": "string",
  "agent_id": "string",
  "tool_name": "read|bash",
  "ok": true,
  "summary": "string",
  "detail_ref": "string"
}
```

```json
{
  "type": "error",
  "session_id": "string",
  "agent_id": "string",
  "code": "string",
  "message": "string"
}
```

#### 6.4.3 Gateway <-> Agent（内部契約）

Agent実行要求:

```json
{
  "request_id": "string",
  "session_id": "string",
  "agent_id": "string",
  "input_text": "string",
  "allowed_tools": ["read", "bash"],
  "timeout_sec": 120
}
```

Agent応答:

```json
{
  "request_id": "string",
  "status": "ok|error",
  "assistant_text": "string",
  "tool_calls": [
    {
      "tool_name": "read|bash",
      "args": {},
      "result_summary": "string",
      "ok": true
    }
  ]
}
```

契約ルール:

- `request_id` は冪等キーとして扱い、重複要求は同一結果を返す。
- `allowed_tools` にないツール呼び出しはGatewayが拒否し、`error` を返す。
- すべてのメッセージに `session_id` と `agent_id` を含める。

## 7. 実装ロードマップ

- Phase 1: 最小動作系（Linux, 単一エージェント, read+bash, Anthropic）
- Phase 2: シングルエージェント強化（write/edit, マルチプロバイダ, レスポンシブ）
- Phase 3: マルチエージェント + オーガナイザー
- Phase 4: 復旧・分離・外部公開強化

## 8. DoD

### 8.1 Phase 1 必須合格

- E2E 10シナリオ成功
- サンドボックス脱出試行 0件成功
- 重大脆弱性 0件
- クラッシュ率 0件（1時間連続使用）
- 積極的フラッシュ成功（70%閾値）

### 8.2 Phase 1 後追い可（Phase 1.1）

- コアカバレッジ80%
- 1週間日常使用でブロッキングバグなし

## 9. 非スコープ（Cut）

- 音声
- Canvas/A2UI
- ClawHubスキルレジストリ
- モバイルネイティブアプリ
