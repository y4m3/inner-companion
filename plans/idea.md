# OpenClaw 調査レポート — 模倣・洗練のための機能・体験要件（背景資料）

> [!IMPORTANT]
> このファイルは **背景資料（調査・レビュー履歴）** です。  
> 実装時の正本仕様（Source of Truth）は `plans/spec.md` を参照してください。  
> 本ファイルと `plans/spec.md` が矛盾する場合は、`plans/spec.md` を優先します。

関連:
- 正本仕様: `plans/spec.md`
- 調査/レビュー履歴: 本ファイル（`plans/idea.md`）

## Context

OpenClaw: ローカルファーストAIアシスタント。430,000+行TypeScript、36拡張、51スキル。
問題: 過剰な複雑さ、Docker展開時のDinD問題、セキュリティopt-in設計（Ciscoが脆弱性実証済み）。

本レポートは「OpenClawを模倣・洗練させるとき、何を提供すべきか」を整理する。

**アーキテクチャ原則**:
- **ホスト直接実行**: システム本体はDocker内ではなくホスト上で直接実行する。Dockerでシステムを立ち上げると、エージェントのアプリ開発でDockerを前提にできなくなる（DinD問題）
- **OS優先順位**: Linux（Tier 1）→ macOS（Tier 2）→ Windows（Tier 3）。Linux向けに設計し、macOS/Windowsは段階的に対応

---

## 1. OpenClawから残すべき核心

| 設計 | 内容 |
|------|------|
| Gateway-Agent パターン | WebSocket制御プレーン。全クライアントのハブ |
| 最小ツール哲学 | read/write/edit/bash の4ツール。自己拡張 |
| 永続メモリ | 日次ログ + MEMORY.md + セマンティック検索 + コンテキスト圧縮前フラッシュ |
| セッション永続化 | append-only JSONL。分岐管理はparent_idフィールドで表現 |
| マルチエージェント | レーン別FIFOキュー。サブエージェント生成。エージェント間通信 |

---

## 2. UX設計

### 2.0 ビジョン: パーソナルAI

近未来SFに出てくるような**「自分専用のAI」**。ひとりひとりにAIがいて、主人にカスタマイズされていく存在。

**目指す体験**:
- **1つの人格的インターフェース**と話す。裏で何が動いているかはユーザーは意識しない
- 主人の好み・癖・文脈を覚えて育っていく
- 頼んだことを自律的にやり、終わったら報告してくる
- 頼んでいないことでも、気づいたら教えてくれる

**体験シナリオ**:

```
朝:
  ユーザー: おはよう
  AI: おはようございます。昨晩、project-xのPR #42にレビューコメントが来ていました。
      あと、今日15時にミーティングがあります。議題を先に整理しておきましょうか？

日中:
  ユーザー: あのバグ直しといて
  AI: project-xの#123の件ですね。修正作業を始めます。
      [コーディングエージェントが別タブで作業開始]
      完了したらお知らせします。

夜:
  AI: (通知) project-xのビルドが通りました。差分のサマリーを置いておきます。
      明日の朝、確認してください。
```

**現実の制約と対応**:
- 現時点のLLMでは「あれやっといて」でオーガナイザーが完璧に振り分けるのは難しい
- → **普段はオーガナイザーと話す（理想）+ 必要なときは個別エージェントに直接切り替え可能（現実の逃げ道）**
- オーガナイザーは「ホーム画面」的な存在であり、強制的なゲートキーパーにはしない

### 2.1 オーガナイザーAI + ユーザー定義エージェント

人間は**オーガナイザーAI（パーソナルAI）** と主に会話する。
オーガナイザーが必要に応じてエージェントを起動・管理する。
ただし、**人間はいつでも個別エージェントに直接指示を出せる**。

**エージェントはプリビルトの機能ではなく、ユーザーが自由に定義するもの。**
プラットフォームはエージェントの実行基盤（フレームワーク）を提供し、具体的なエージェントの種類・振る舞いはユーザーが定義ファイル（YAML）またはWebChat UIから作成する。

```
人間 ←→ [オーガナイザーAI（パーソナルAI）] ← 普段の対話相手
              |
              ├─→ [Agent A: コーディング]     ← 人間が直接タブ切り替えで操作可能
              ├─→ [Agent B: リサーチ]         ← 結果をオーガナイザーに返す
              ├─→ [Agent C: タスク管理(常駐)]  ← Cron/Webhook駆動
              └─→ [Agent D: ニュース巡回(常駐)] ← 朝のブリーフィングを生成
```

**体験フロー**:
1. 人間がオーガナイザーに依頼（例:「このバグ直して」）
2. オーガナイザーが適切なエージェントを起動→新しいチャット画面（タブ）を作成
3. 人間は通知バッジで認識し、ワンクリックでそのタブに遷移
4. **人間は必要に応じてエージェントに直接指示**を出せる（オーガナイザーを介さない）
5. 完了後、結果がオーガナイザーのチャットにサマリーとして返る
6. 常駐エージェントの収集結果は「朝のブリーフィング」としてオーガナイザーがまとめて報告

**エージェントの動作モード**（定義ファイルで指定）:
- **タスク起動型**: 依頼を受けて起動→完了後にオーガナイザーへ結果を返す。例: コーディング、リサーチ
- **常駐型**: バックグラウンドで常時稼働。Cron/Webhook/イベント駆動。例: タスク管理、ニュース巡回。結果はオーガナイザーのブリーフィングに集約
- **完全自律型**: 人間の介入なしで動作。定義ファイルで自律性レベル（ReadOnly/Supervised/Full）を設定

### 2.2 セッション検索・再開UX

過去のチャットを簡単に見つけて再開できる仕組み:

| 機能 | 説明 |
|------|------|
| セマンティック検索 | 「100日前のあのプロジェクト」のようなあいまいな検索で発見 |
| 自動タグ付け | 各セッションにAIが自動でトピックタグ・サマリーを付与 |
| タイムライン表示 | 日付・プロジェクト別のビジュアルタイムライン |
| オーガナイザー経由再開 | 「前やったXXの続きやりたい」→オーガナイザーが該当セッションを特定して再開 |
| ピン留め | 重要セッションをピン留めして常にアクセス可能 |

### 2.3 レスポンシブUI（PC + iPhone対応）

- WebChat UIはPC・iPhone両対応のレスポンシブ設計
- PWA対応でホーム画面追加可能
- 既存メッセンジャーアプリとの接続は不要（専用WebChat UIで完結）

**UI構造: オーガナイザー = ホーム画面**

```
PC（マルチペイン）:
┌──────────┬──────────────────────────────┐
│ サイドバー │  メインペイン                   │
│           │                              │
│ 🏠 ホーム  │  [オーガナイザー会話]           │
│ 🔨 coding │                              │
│ 📋 tasks  │  または                       │
│ 📰 news   │                              │
│ ─────── │  [選択中のエージェント会話]      │
│ + 新規作成 │                              │
│           │                              │
│ 通知バッジ │  右ペイン: エージェント出力     │
│ で状態表示 │  （PC時のみ。分割表示可能）    │
└──────────┴──────────────────────────────┘

iPhone（タブ切り替え）:
┌──────────────────────┐
│ [🏠] [🔨] [📋] [📰]   │ ← 下部タブバー。通知バッジ付き
├──────────────────────┤
│                      │
│  選択中の会話画面      │
│                      │
└──────────────────────┘
```

**操作フロー**:
- **オーガナイザー経由**: ホーム画面で依頼→エージェントが起動→サイドバー/タブに新エージェントが出現（通知バッジ付き）→クリック/タップで遷移
- **直接操作**: サイドバー/タブから任意のエージェントを直接選択→そのエージェントに直接指示を出す（オーガナイザーを介さない）
- **オーガナイザーへの復帰**: 🏠ホームをクリック/タップでいつでもオーガナイザーに戻る

### 2.4 エージェント間データ共有

| 共有対象 | 方法 |
|----------|------|
| 会話履歴 | 読み取り専用で他エージェントの履歴を参照可能 |
| 共有メモリ | 全エージェント共通のナレッジベース（SQLite） |
| ファイル | 共有ワークスペースディレクトリ。書き込みはGateway経由でチェックサム記録。読み取り時に整合性検証 |
| タスク | タスクDB（SQLite）で一元管理。全エージェントがタスクの作成・更新が可能 |

**bashツール時の整合性記録（明文化）**:
- bashはGatewayが起動するサンドボックス内で実行される。
- bash実行後、Gatewayは変更ファイルを検出してチェックサムを監査ログへ追記し、`Gateway経由でチェックサム記録`の要件を満たす。
- これにより「直接FS変更」と「Gateway監査」を両立する。

### 2.5 エージェント定義フレームワーク

エージェントはプラットフォームの組み込み機能ではなく、**ユーザーが定義ファイルまたはWebChat UIから自由に作成する**。

**作成方法（2通り）**:

| 方法 | 対象ユーザー | 説明 |
|------|-------------|------|
| **YAML定義ファイル** | パワーユーザー | `/data/agent-config/` に配置。直接編集・Git管理可能 |
| **WebChat UIウィザード** | 全ユーザー | 対話形式でエージェントを作成。内部的にYAMLファイルを生成 |

**オーガナイザーAI（パーソナルAI）の定義ファイル例**:

```yaml
# /data/agent-config/_organizer.yaml  ← 特殊: アンダースコア始まりでオーガナイザーとして認識
name: "organizer"
description: "Personal AI assistant"
model: "claude-sonnet-4-5-20250929"

# パーソナリティプロファイル（パーソナルAIの人格設定）
personality:
  name: "アシスタント"           # AIの呼び名（ユーザーが自由に設定）
  tone: "friendly-professional"  # "casual" | "friendly-professional" | "formal"
  language: "ja"                 # 応答言語
  traits: |                      # 自由記述の性格設定。system_promptに組み込まれる
    - 簡潔に答える。冗長にならない
    - 技術的な話題では正確さを重視
    - ユーモアは控えめ
  user_context: |                # ユーザーについての固定情報。AIが主人を理解するための基盤
    - ソフトウェアエンジニア。Go, TypeScript, Rustを使う
    - 朝型。作業は午前中に集中する
    - コードレビューは厳しめを好む

# オーガナイザー固有設定
organizer:
  briefing:
    enabled: true
    schedule: "0 7 * * *"          # 毎朝7時にブリーフィング生成
    sources: ["tasks", "news", "git"]  # 収集対象: タスクDB、常駐エージェント結果、git活動
  context_resolution:
    short_term_sessions: 5         # 「あれ」「それ」の解決に直近N件のセッションを参照
    task_db: true                  # タスクDBからも文脈を解決

system_prompt: |
  You are the user's personal AI assistant.
  Your personality and user context are defined above.
  You manage other agents on behalf of the user.
  When the user says something ambiguous, resolve it from recent sessions and task DB.
  Generate morning briefings from resident agent results.

mode: "resident"
resident:
  cron: "0 7 * * *"    # 朝のブリーフィング
  idle_timeout: "0"     # 常時起動（タイムアウトなし）

autonomy: "supervised"
tools:
  allowed: ["read"]     # オーガナイザー自身は読み取りのみ。作業は専門エージェントに委任
memory:
  shared: true
  private_namespace: "organizer"
```

**一般エージェント定義ファイル（YAML）のスキーマ例**:

```yaml
# /data/agent-config/coding-agent.yaml
name: "coding-agent"
description: "General-purpose coding assistant"
model: "claude-sonnet-4-5-20250929"    # デフォルトモデル
system_prompt: |
  You are a coding assistant. You help with software development tasks.
  Always explain your reasoning before making changes.

# 動作モード
mode: "task"            # "task" | "resident" | "autonomous"

# タスク起動型の設定（mode: "task"）
task:
  auto_start: true      # オーガナイザーが自動起動可能か
  create_chat: true     # 起動時に独立チャット画面を作成するか
  return_summary: true  # 完了時にオーガナイザーへサマリーを返すか

# 常駐型の設定（mode: "resident"）
# resident:
#   cron: "0 */6 * * *"       # 6時間ごとに起動
#   webhooks: ["/hook/news"]  # Webhook受信で起動
#   idle_timeout: "30m"       # アイドル時のタイムアウト

# ツール権限
tools:
  allowed: ["read", "write", "edit", "bash"]
  # bash_whitelist: ["git", "npm", "go"]   # オプション: コマンドホワイトリスト

# 自律性レベル
autonomy: "supervised"   # "readonly" | "supervised" | "full"

# Capability制限
capabilities:
  max_file_changes_per_session: 20
  allowed_paths: ["/workspace/my-project"]
  network_allowlist: ["api.anthropic.com", "api.openai.com"]

# メモリ設定
memory:
  shared: true           # 共有メモリへのアクセス
  private_namespace: "coding"  # プライベートメモリの名前空間
```

**WebChat UIでの作成フロー**:
1. 「新規エージェント作成」ボタン → ウィザード開始
2. 名前・説明・モデル選択
3. システムプロンプト入力（テンプレートから選択 or 自由記述）
4. 動作モード選択（タスク起動型 / 常駐型 / 完全自律型）
5. 常駐型の場合: Cronスケジュール / Webhookエンドポイント設定
6. ツール権限・自律性レベル設定
7. 確認 → YAMLファイルとして `/data/agent-config/` に保存

**プラットフォームが提供するもの**:
- エージェント実行基盤（プロセス管理、サンドボックス、メモリ、セッション）
- オーガナイザーAI（ユーザーの意図を解釈し、適切なエージェントを選択・起動）
- 定義ファイルテンプレート集（コーディング、リサーチ等の一般的なテンプレートを同梱）
- Cron/Webhookスケジューラ（常駐型エージェントの起動トリガー）

**プラットフォームが提供しないもの**:
- 特定用途のエージェント（「タスク管理Agent」「ニュース配信Agent」等はユーザーが必要に応じて作成）

### 2.6 コンテキスト管理戦略

LLMのコンテキストウィンドウは有限。長い会話で2つの問題が発生する:
- **A. 永続化前消失**: コンテキストが満杯→古い情報が圧縮・破棄される前にメモリに保存されないリスク
- **B. 性能低下**: コンテキストが長くなるほどLLMの注意力が分散→指示の見落とし・品質低下

**コンテキスト3層モデル**:

```
┌─────────────────────────────────────┐
│ Layer 1: システムプロンプト（固定）      │  ← 人格設定 + エージェント定義 + ツール定義
│          ~2-4K tokens               │     圧縮対象外
├─────────────────────────────────────┤
│ Layer 2: メモリ要約（半固定）          │  ← MEMORY.md + 直近セッション要約
│          ~2-8K tokens               │     セッション開始時に構築
├─────────────────────────────────────┤
│ Layer 3: 会話履歴（動的）             │  ← 直近の会話ターン
│          残りのコンテキスト枠を使用     │     古いターンから圧縮・破棄
└─────────────────────────────────────┘
```

**対策**:

| 問題 | 対策 | 実装Phase |
|------|------|-----------|
| **永続化前消失** | **積極的フラッシュ**: コンテキスト使用率が閾値（70%）を超えたら、LLMに「重要情報をメモリに書き出して」と指示。圧縮前に必ず永続化 | Phase 1 |
| 同上 | **構造化自動記録**: ツール実行結果・決定事項は会話フローとは別にDBに自動記録（LLMの要約に依存しない確実な永続化） | Phase 1 |
| 同上 | **セッション終了時要約**: セッション終了（明示的 or タイムアウト）時にLLMが会話全体を要約→MEMORY.mdに追記 | Phase 1 |
| **性能低下** | **二面的ToolResult**: ツール出力はForLLM（簡潔）/ ForUser（リッチ）に分離。LLMコンテキストに入るのは簡潔版のみ | Phase 1 |
| 同上 | **早期切り詰め**: 長いbash出力・ファイル内容は先頭/末尾N行のみLLMに渡す。全文はUI側に表示 | Phase 1 |
| 同上 | **セッション分割**: 長すぎる会話（コンテキスト80%超が3回連続）を自動的に新セッションに分割。前セッションの要約を引き継ぎ | Phase 2 |
| 同上 | **階層的コンテキスト管理**: Layer 1-3のバランスをGatewayが動的に調整。Layer 3が圧迫されたらLayer 2の要約を短縮 | Phase 2 |
| **コスト** | **コンテキスト効率追跡**: セッション別のトークン消費をモニタリング。無駄なコンテキスト消費パターンを検知→ユーザーに通知 | Phase 2 |

**70%積極的フラッシュの責務分離（明文化）**:
- 閾値監視: `internal/session` がコンテキスト使用率を計測し、70%超過イベントを発火する。
- フラッシュ実行: `internal/agent` がLLMへメモリ書き出し指示を行う。
- 永続化: `internal/memory` が構造化データ（SQLite）と要約（MEMORY.md）へ保存する。
- 監査: `internal/gateway` がフラッシュ発火時刻・対象セッションID・結果を監査ログへ記録する。

**「育つAI」のためのメモリ成長サイクル**:

```
会話 → 重要情報をメモリに書き出し（積極的フラッシュ）
     → セッション終了時に要約生成
     → MEMORY.mdに蓄積
     → 次回セッション開始時にLayer 2として読み込み
     → AIが主人の好み・パターンを徐々に学習
     → パーソナリティプロファイル(user_context)の自動更新提案（Phase 3）
```

---

## 3. 技術選定（改訂）

### 3.1 Go + TypeScript ハイブリッド（推奨に変更）

**Bunの問題点**（調査結果）:
- 後方互換性ポリシーが「事後対応」。破壊的変更が頻繁
- Windows: セグフォルトが安定版でも多発（Claude Codeでも問題発生中）
- Anthropicが2025年12月にBunを買収 → 内部用途に最適化される可能性。セルフホスト用途では不確実

**Goの利点**:
- **Go 1互換性保証**: 2012年以降、書いたコードは将来も動く。Go 2は出ない
- **サンドボックス**: Linux namespaces, seccomp-bpf, cgroupsをネイティブに操作可能（go-sandbox, go-seccomp-bpf）
- **シングルバイナリ**: `GOOS=windows go build` でクロスコンパイル。依存なし
- **WebSocket**: gorilla/websocket, nhooyr/websocket が成熟
- **AI SDK**: anthropic-sdk-go（公式）、Google ADK for Go
- **goroutine**: 数千のエージェント接続を効率的に管理

| コンポーネント | 選定 | 理由 |
|----------------|------|------|
| **バックエンド** | **Go** | 安定性、サンドボックス、シングルバイナリ、goroutine並行処理 |
| **フロントエンド** | **TypeScript + Svelte/Preact + Vite** | UI開発速度。GoとはWebSocket/REST APIで接続 |
| WebSocket | nhooyr/websocket (coder/websocket) | Go標準。context.Context対応 |
| HTTP | Go標準net/http + chi router | 軽量。外部依存最小 |
| DB | SQLite（**modernc.org/sqlite**） | **CGO不要。ピュアGo。シングルバイナリ方針と整合** |
| 設定 | YAML + go-playground/validator | Go標準的手法 |
| サンドボックス | **go-landlock（公式）+ go-seccomp-bpf** | カーネルネイティブ。root不要。Go統合◎ |

### 3.2 バックエンドにTypeScriptを不採用にした理由

- Node.js/Bunではbash実行のサンドボックスが**本質的に不可能**（OS syscallを直接操作できない）
- Go ならseccomp-bpf フィルタをGoコードから直接設定してchild processに適用できる
- AIモデル呼び出しはHTTP APIなので言語の優位性は関係ない
- フロントエンドはTypeScriptで作り、GoバックエンドとWebSocket/APIで通信する（業界標準パターン）

---

## 4. セキュリティアーキテクチャ（全面改訂）

### 4.0 軽量脅威モデル

個人用途（盆栽プロジェクト）のため、企業レベルの脅威モデリング（STRIDE等）ではなく、**実害ベースの最小脅威モデル**を定義する。

**守る資産**:

| 資産 | 影響 | 優先度 |
|------|------|--------|
| ローカルファイルシステム | ワークスペース外のファイル破壊・漏えい | **最高** |
| APIキー・シークレット | LLM API / 外部サービスの不正利用・課金被害 | **最高** |
| 会話履歴DB | プライベートな会話内容の漏えい | 高 |
| 設定ファイル | サンドボックス設定の改ざん→制限無効化 | 高 |
| ホストOS | 権限昇格・システム破壊 | 高 |

**攻撃者像（Threat Actor）**:

| 攻撃者 | 動機 | 能力 |
|--------|------|------|
| **悪意あるプロンプト（最も現実的）** | LLMを騙して危険操作を実行させる | prompt injection、tool乱用。LLM経由で間接的にシステムを操作 |
| **悪意あるMCPサーバ/依存パッケージ** | サプライチェーン汚染 | 任意コード実行。ただしサンドボックス制限下 |
| **ネットワーク上の攻撃者** | APIキー窃取、セッション乗っ取り | 同一LAN上の盗聴（127.0.0.1バインドで緩和） |

**攻撃経路と対策**:

| # | 攻撃経路 | 対策 | 実装Phase |
|---|----------|------|-----------|
| T1 | Prompt injection → `rm -rf /` 等の破壊的コマンド | Landlock（ワークスペース外アクセス不可）+ 破壊的操作の人間承認（L2/L3分類） | Phase 1 |
| T2 | Prompt injection → APIキー読み取り（`cat ~/.config/...`） | Landlock（設定ディレクトリへのアクセス不可）+ シークレットのAEAD暗号化 | Phase 1 |
| T3 | Tool連鎖で小さな操作の積み重ね → 実質的破壊 | 累積閾値（10ファイル変更/セッション）超過で人間承認要求 | Phase 1 |
| T4 | サンドボックス脱出（ptrace, memfd_create等） | seccomp-bpfで危険syscallブロック + PR_SET_NO_NEW_PRIVS | Phase 1 |
| T5 | fork爆弾 / リソース枯渇 | cgroups v2（PID上限256, メモリ512MB, CPU 1コア） | Phase 1 |
| T6 | エージェント間プロンプト伝播 | 構造化メッセージ + Capability証明書 + エージェント間読み取りスコープ制限 | Phase 2 |
| T7 | 悪意あるMCPサーババイナリ | SHA256ハッシュ検証 + Landlock/seccomp制限下実行 | Phase 1 |
| T8 | ネットワーク盗聴 | 127.0.0.1バインド + Tailscale（Phase 3） | Phase 1/3 |

**対策の限界（正直な認識）**:
- Prompt injectionは**完全には防げない**。構造化とサンドボックスで**被害を限定する**設計
- Landlockはdefense-in-depthの一層であり、**絶対的な境界ではない**（カーネルチーム公式見解）
- 個人開発では全攻撃経路の網羅的テストは現実的でない。**T1-T5（Phase 1対象）を優先**

### 4.1 根本的な問題認識

> アプリケーション層のパスガードは**バイパス可能であることが一番の問題**。
> エージェントがbashで`cat /etc/shadow`を実行すれば突破される。
> さらにエージェント間で自由にプロンプトをやり取りする → 人間が制御しきれない。

**これは正しい。OpenClawを含むほとんどのAIエージェントプラットフォームが抱える根本的な弱点。**

### 4.2 Linuxサンドボックス技術の比較

| ツール | 仕組み | root要否 | 粒度 | Go統合 | 適性 |
|--------|--------|----------|------|--------|------|
| **Landlock** | カーネルネイティブ。プロセス自己制限 | **不要** | ファイル制御: 5.13+、TCP制御: 6.7+ | **go-landlock（公式）** | **最適** |
| **nsjail** | namespace + cgroups + seccomp-bpf | 要（CAP_SYS_ADMIN） | プロセス全体 | os/exec経由 | 強力だが重い |
| **bubblewrap** | namespace + seccomp | 不要（user ns） | マウント単位 | os/exec経由 | 低レベル。ラッパー必要 |
| **Firejail** | suid + chroot + namespace + seccomp | 要（suid） | プロファイル単位 | os/exec経由 | デスクトップ向け |
| **landrun** | Landlock v5ラッパー | **不要** | ファイル + TCP | CLI経由 | 軽量だがリソース制限なし |
| **gVisor** | ユーザ空間カーネル | 要 | syscall全体 | Docker runtime | 重量級。高分離 |

**推奨: Landlock + seccomp-bpf の組み合わせ（Linux Tier 1）**

理由:
- **root不要**: 非特権ユーザーでプロセスが自身を制限可能。nsjailのCAP_SYS_ADMIN問題を回避
- **カーネルネイティブ**: 外部ツール不要。ファイル制御はLinux 5.13+（Ubuntu 22.04+, Debian 12+）、TCP制御は6.7+（Ubuntu 24.10+, Debian trixie+）が必要。TCP非対応カーネルではアプリ層のネットワーク許可リスト（Go net/http Transport + DialContext制限）でフォールバック。iptablesはroot要のため非推奨
- **Go/Rust公式ライブラリ**: `go-landlock`（Go）、`rust-landlock`（Rust）ともに公式。Node.jsにはバインディングなし → **GoまたはRustが技術的必然**
- **低オーバーヘッド**: namespace分離より軽量。AIエージェントの応答速度に影響しない
- **階層的制限**: 制限は子プロセスに自動継承され、緩和不可能（権限エスカレーション防止）
- **seccomp-bpfと併用**: Landlockでファイル/ネットワーク制御 + seccompでsyscall制限 = OpenAI Codexと同等の防御

**Phase 1でのLinuxネットワーク制限の固定方針（明文化）**:
- Landlock TCP制御が使えないカーネルでは、**Go `net/http` の `Transport.DialContext` 制限を唯一の強制手段**として採用する。
- Phase 1はこの方式で `network_allowlist` のホスト制限を実施し、iptables/nftables依存は採用しない（root不要方針を維持）。
- Landlock TCP制御（6.7+）は利用可能環境で追加の防御層として有効化する。

**Landlock単体の限界（seccomp併用が必須な理由）**:
- ptrace経由で他プロセスを操作→サンドボックス脱出が可能 → **seccompでptraceをブロック**
- memfd_create経由でメモリ内実行可能ファイルを作成可能 → **seccompでmemfd_createをブロック**
- symlink/hardlink経由のパス走査 → **PR_SET_NO_NEW_PRIVS + 非rootユーザー実行**
- /proc/self/fd/ 経由のFDアクセス → 制限付きだが完全ブロック困難（defense-in-depthで対応）
- カーネルチームも「defense-in-depthの一層として設計」と明言。**絶対的な境界ではない**

**ブラウザ自動化にはLandlockは不適合**:
- Chromiumは /dev/shm, /tmp, /dev/dri/* への書き込みが必須
- Chromium自身がseccomp-bpfを内部使用 → Landlockと二重サンドボックスが競合
- → **ブラウザ操作はDocker/gVisorコンテナに委任**（DinDではなく、Agent本体はホスト上）

### 4.2.1 macOSサンドボックス戦略（Tier 2）

macOSにはLandlock/seccompに相当するカーネルネイティブAPIが存在しない。以下の選択肢を段階的に採用する。

| ツール | 仕組み | 状態 | Go統合 | 適性 |
|--------|--------|------|--------|------|
| **sandbox-exec** | Seatbelt（TrustedBSD MAC）プロファイル。カーネルレベル強制 | **非推奨（deprecated）** だが動作可能。OpenAI Codex CLI、Google Gemini CLIが現在も使用中 | os/exec経由でプロファイル渡し | Phase 1の暫定策。実績あり |
| **Alcoholless** | NTT研究所製。macOS向け軽量セキュリティサンドボックス。AIエージェント用途を想定 | アクティブ開発中 | os/exec経由 | sandbox-execの代替候補 |
| **macOS 26 Containerization** | WWDC 2025発表。ネイティブLinuxコンテナ対応。コンテナ毎のVM分離、サブ秒起動、Apple Silicon最適化 | macOS 26+（2025秋〜） | Apple Swiftフレームワーク。Go連携要調査 | 将来の本命。完全分離 |
| **アプリ層ガード** | コマンドホワイトリスト + MCP Server経由のみ | 常に利用可能 | ネイティブ | 最低限の制御 |

**推奨戦略**:
- **Phase 1**: sandbox-exec（Seatbelt）プロファイルでファイル/ネットワーク制限 + コマンドホワイトリスト。deprecatedだが実績多数（Codex CLI/Gemini CLIが依存）
- **Phase 2**: Alcoholless評価。sandbox-execより安定したAPIなら切り替え
- **将来**: macOS 26 Containerizationが普及した時点で、Linuxと同等のコンテナ分離を検討

**sandbox-execプロファイル例**:
```scheme
(version 1)
(deny default)
(allow file-read* (subpath "/usr") (subpath "/lib") (subpath "/bin"))
(allow file-read* file-write* (subpath "/workspace/project-a"))
(allow file-read* file-write* (subpath "/data/memory"))
;; LLM API接続先のみ許可（*:443ではなくホスト限定）
(allow network-outbound (remote tcp "api.anthropic.com:443"))
(allow network-outbound (remote tcp "api.openai.com:443"))
(allow network-outbound (remote tcp "openrouter.ai:443"))
;; エージェント定義のnetwork_allowlistからプロファイル生成時に動的追加
(deny network-inbound)
```

### 4.3 防御戦略: Landlock + seccomp-bpf + Capability Gateway

```
人間 ←→ [オーガナイザーAI]
              |
              ├─→ [Agent Process]
              |       ├─ Landlock: ワークスペースのみ読み書き可
              |       ├─ Landlock: /etc, /root 等はアクセス不可
              |       ├─ Landlock: TCP接続先を許可リストに制限
              |       ├─ seccomp-bpf: 危険なsyscall（ptrace等）をブロック
              |       └─ cgroups v2: CPU/メモリ/時間制限
              |
              └─→ [ブラウザ操作] → Docker/gVisorコンテナ
                      └─ ホストとファイルシステム完全分離
```

**具体的な防御層**:

| 層 | 手法 | 防御対象 | フェーズ |
|----|------|----------|----------|
| **L1: ネットワーク** | 127.0.0.1バインド + Tailscale | 外部アクセス制御 | **Phase 1** |
| **L2: 認証** | Gatewayペアリングトークン（Tailscale IDはPhase 3で追加） | 不正接続 | **Phase 1** |
| **L3: ファイル制御** | **Landlock**（Linux）/ **sandbox-exec**（macOS）/ コマンドホワイトリスト（Windows） | ワークスペース外アクセス防止 | **Phase 1** |
| **L4: syscall制御** | **seccomp-bpf**（Linux）/ sandbox-exec Seatbelt（macOS） | 危険なsyscall（ptrace, mount等）ブロック | **Phase 1** |
| **L5: リソース制限** | **cgroups v2**（PID/IO含む。§4.5参照） | CPU/メモリ/PID/IO/実行時間の上限 | **Phase 1** |
| **L6: Capability制御** | MCP Server経由 + コマンドホワイトリスト | 構造化ツール呼び出しのみ許可 | **Phase 2** |
| **L7: 監査** | append-only JSONL + チェーンハッシュ（§4.5参照） | 事後検知・改ざん検知 | **Phase 2** |
| **L8: 復旧** | スナップショット + バックアップ | 破壊からの回復 | **Phase 3** |
| **L9: 高リスク分離** | Docker/gVisor | ブラウザ操作等の完全分離 | **Phase 3** |

**Phase 1で必須の層**: L1-L5（最小限の安全なエージェント実行環境）
**Phase 2で追加**: L6-L7（マルチエージェント時の制御と監査）
**Phase 3で追加**: L8-L9（運用成熟後の復旧・高リスク分離）

**Landlockの実装イメージ（Go）**:
```go
import "github.com/landlock-lsm/go-landlock/landlock"

// エージェントプロセス起動前に制限を適用
err := landlock.V5.BestEffort().RestrictPaths(
    landlock.RODirs("/usr", "/lib", "/bin"),       // 読み取り専用
    landlock.RWDirs("/workspace/project-a"),         // ワークスペースのみ読み書き
    landlock.RWDirs("/data/memory", "/data/sessions"), // メモリ・セッションデータ
    // /etc, /root, /home は指定しない → アクセス不可
)
```

### 4.4 エージェント間プロンプトインジェクション対策

エージェントAがエージェントBに指示を出す際、それは「信頼できないプロンプト」である。

> **重要**: Prompt injectionは構造化だけでは**完全には防げない**。以下の対策は被害を**低減（mitigate）** するものであり、防止（prevent）を保証するものではない。最後の砦はサンドボックス（Landlock+seccomp）による被害限定と、高リスク操作の人間承認（§4.0 T1-T3参照）。

**対策（入力サニタイズ + 出力検証の2段構え）**:

**入力側（被害低減）**:
1. **構造化メッセージ + フィールド分離**: JSON schemaでエージェント間通信を定義。`instruction`フィールド（信頼済み、Gatewayが生成）と`data`フィールド（非信頼、ユーザー/エージェント入力）を分離。LLMプロンプト構築時にdataはエスケープ・引用符囲い
2. **Capability証明書**: 各エージェントの実行可能操作をホワイトリスト定義。証明書はGatewayが発行、HMAC-SHA256署名（audience: エージェントID束縛）、TTL付き（デフォルト30分）、使用回数カウンタで再プレイ防止。失効リストはインメモリ + SQLite永続化（Gateway再起動時にSQLiteから復元）。署名鍵はAEAD暗号化マスターキーから導出（§4.5 AEAD鍵管理参照）、ローテーション時は旧鍵での検証を30分間並行実施
3. **権限継承の禁止**: サブエージェントは親の権限を継承しない。Gatewayがサブエージェント用に個別のCapability証明書を発行

**出力側（検証）**:
4. **操作の影響度分類と人間承認（最後の砦）**: 操作をL1(読み取り)/L2(可逆変更)/L3(不可逆変更)に分類。L3は必ず人間承認。L2も累積閾値（例: 10ファイル変更/セッション）を超えたら人間承認を要求。非破壊操作の連鎖による実質的破壊を防止。**Prompt injectionが構造化を突破した場合でも、この人間承認が最終防御線として機能する**
5. **Gatewayでの中央監視**: 全エージェント間通信をGatewayが仲介・監査。異常パターン検知（短時間の大量リクエスト、権限外操作の試行）
6. **エージェント間会話読み取りのスコープ制御**: オーガナイザーAIのみが全エージェント会話を読み取り可能。一般エージェント間は明示的に共有されたサマリーのみ参照可能。APIキー・トークン等のシークレットはログ書き込み前にマスキング

**autonomy / tools / 影響度分類の対応表（明文化）**:

| 操作 | 影響度 | readonly | supervised | full |
|------|--------|----------|------------|------|
| `read` | L1 | 許可 | 許可 | 許可 |
| `bash` (読み取り系: `ls`, `cat`, `rg`) | L1 | 禁止 | 許可 | 許可 |
| `write`, `edit` | L2 | 禁止 | 条件付き許可（累積閾値超過で承認） | 許可 |
| `bash` (可逆変更: `git checkout -b`, `go test`, `npm install`) | L2 | 禁止 | 条件付き許可（累積閾値超過で承認） | 許可 |
| `bash` (不可逆変更: `rm -rf`, `git push`, 外部公開操作) | L3 | 禁止 | 人間承認必須 | 人間承認必須 |

注記:
- `full` でもL3は常に人間承認を必須とし、無承認の不可逆操作は許可しない。
- `supervised` のL2は「1セッション10ファイル変更」または「同等の影響度」に到達した時点で承認を要求する。

### 4.5 セキュリティ詳細設計

**MCPサーバ信頼境界**:
- MCPサーバはGatewayプロセスと同一マシン上で実行。外部MCPサーバ接続は設定で明示的オプトイン
- MCPサーババイナリのSHA256ハッシュを設定ファイルに記載、起動時に検証
- MCPサーバもLandlock/seccomp制限下で実行（Gateway本体より狭い権限）

**不変ログの実装**:
- 監査ログはappend-only JSONL。書き込み専用fd（O_APPEND | O_WRONLY）でオープン
- Landlock制限で監査ログファイルへの書き込みのみ許可（削除・切り詰め不可）
- 日次ローテーション時にSHA256チェーンハッシュを生成（前日ハッシュ + 当日内容）
- 改ざん検知: 起動時にチェーンハッシュを検証。不整合時に警告

**cgroups v2 詳細制限**:
- CPU: 1コア相当（cpu.max: 100000 100000）
- メモリ: 512MB（memory.max）
- PID上限: 256（pids.max）— fork爆弾防止
- I/O帯域: 50MB/s上限（io.max）— IO枯渇防止
- 実行時間: 120秒タイムアウト（アプリ層 + cgroups freezer）

**non-root運用手順（Linux, Phase 1）**:
- cgroups v2はsystemdのdelegation機能を利用して、`systemd --user` 配下の委譲済みcgroupに対して設定する。
- 起動時にGatewayはdelegation可否を検査し、利用可能なら `cpu.max` / `memory.max` / `pids.max` / `io.max` を適用する。
- delegation不可の環境ではPhase 1フォールバックとして、アプリ層の実行時間制限・同時実行制御・プロセス数監視を適用し、`io.max` は無効化を明示ログ出力する。
- 本番運用の必須条件は「delegation有効環境」または「制限付きフォールバックを許容した単一ユーザー運用」のどちらかとする。

**ネットワークセキュリティ**:
- Gateway-Agent間: UNIXドメインソケット（同一マシン前提）
- Gateway-Client間: WebSocket over TLS。Tailscale内ならWireGuardで暗号化済み
- WS再接続: リフレッシュトークンで新セッショントークンを取得（旧トークンは即失効）
- Tailscale外アクセス時: Cloudflare Tunnel + Access（Phase 3）でmTLS相当を実現

**AEAD鍵管理**:
- マスターキー: OS keyring（Linux: libsecret/kwallet、Windows: DPAPI、macOS: Keychain）に格納
- OS keyring非対応環境: パスフレーズ由来鍵（Argon2id KDF、コスト: t=3, m=64MB）
- 鍵ローテーション: 旧鍵で復号→新鍵で再暗号化のマイグレーションコマンドを提供
- 復旧: パスフレーズ + salt から鍵を再導出。saltは設定ファイルに平文保存

**Webhookセキュリティ**:
- Webhookエンドポイントは127.0.0.1バインド（Tailscale経由のみ外部到達可能）
- 署名検証: HMAC-SHA256。共有シークレットはWebhook登録時に生成、AEAD暗号化で保存
- リプレイ防止: タイムスタンプ検証（±5分）+ nonceをSQLiteに記録（24時間保持後パージ）
- 送信元認証: Tailscale内ではTailscale whois。ローカルのみ時は署名検証のみで十分
- 失敗時再送ポリシー: Webhook受信側（本システム）は受信即応答（202 Accepted）。処理失敗はキューに再投入（最大3回）
- 鍵ローテーション: 旧シークレットと新シークレットを並行検証（グレースピリオド24時間）

**macOS セキュリティ方針**（Tier 2）:
- sandbox-exec（Seatbelt）プロファイルでbash実行を制限。deny-defaultポリシー
- ファイルアクセス: ワークスペース + 必要な読み取り専用パス（/usr, /lib, /bin）のみ許可
- ネットワーク: sandbox-execプロファイルでLLM API接続先ホスト（api.anthropic.com等）のみ許可。エージェント定義のnetwork_allowlistから動的生成
- cgroups v2相当のリソース制限はmacOSに存在しないため、アプリ層で実行時間120sタイムアウトを適用
- 将来: Alcoholless / macOS 26 Containerizationへの移行パスを維持

**Windows セキュリティ方針**（Tier 3）:
- bash/PowerShell直接実行は禁止（MCP Server経由のCapabilityアクセスのみ）
- MCP Server経由であっても、コマンドホワイトリストで実行可能コマンドを制限
- PowerShell実行ポリシー: Restricted（スクリプト実行禁止）。必要時はAllSignedに変更
- Windows Sandbox / WSL2内での実行をNice-to-Haveとして検討

### 4.6 bashツールの扱い

**問題**: bash直接アクセスはサンドボックスを突破する最大のリスク

**選択肢**:

| 方式 | セキュリティ | 柔軟性 | root要否 | 推奨 |
|------|-------------|--------|----------|------|
| A: bashなし。MCP Server経由のみ | 最高 | 低い | 不要 | Windows環境 |
| B: Landlock+seccomp内でbash実行 | 高い | 高い | **不要** | **Linux推奨** |
| B': sandbox-exec内でbash実行 | 中〜高 | 高い | **不要** | **macOS推奨** |
| C: nsjail内でbash実行 | 高い | 高い | 要 | root利用可能な場合 |
| D: Docker内でbash実行 | 高い | 高い | 要 | DinD以外 |
| E: アプリ層ガードのみ | 低い | 最高 | 不要 | **非推奨** |

**推奨: LinuxではB（Landlock + seccomp-bpf）。root不要でdefense-in-depthの中核層を実現（完全分離ではなく多層防御の一層として機能）。**
**macOS: B'（sandbox-exec Seatbelt）。deprecatedだがCodex/Geminiが依存。将来はAlcohollessまたはmacOS 26 Containerizationに移行。**
**Windows: A（MCP Server経由）。Landlockは使えないため、構造化ツールアクセスのみ。**


## 5. 実装仕様の参照先（正本）

本ファイルは背景資料であり、実装判断に使う仕様は `plans/spec.md` を正本とする。

- 機能仕様（Must-Have/Phase/MVP）: `plans/spec.md`
- セキュリティ仕様（L1-L3、承認ルール、sandbox方針）: `plans/spec.md`
- コンテキスト/メモリ責務: `plans/spec.md`
- 運用仕様（エラー処理、読み取り専用モード、JSONL互換）: `plans/spec.md`
- I/O契約（Phase 1メッセージスキーマ）: `plans/spec.md`
- DoD（受け入れ基準）: `plans/spec.md`

運用ルール:
- 仕様変更は `plans/spec.md` のみ更新する。
- `plans/idea.md` には、変更理由・比較検討・レビュー履歴のみ追記する。

## 6. 背景資料（調査・レビュー履歴）

以下は設計判断の背景資料として保持する（非正本）。

## 7. 類似プロジェクト調査と設計への反映

4つの類似プロジェクトを調査し、採用すべきパターンを特定した。

### 7.1 PicoClaw（Go, ~8,000 LOC）

**概要**: Go製の超軽量OpenClaw代替。<10MB RAM、<1s起動。シングルバイナリ。

**注目すべき設計パターン**:

| パターン | 内容 | 本プロジェクトへの反映 |
|----------|------|----------------------|
| **Message Bus** | `pkg/bus/bus.go` — Pub-Sub + バッファ付き非同期キュー。チャネルとエージェントコアを疎結合化 | **採用**。Gateway内のイベント配信に適用 |
| **非同期ToolCallback** | ツール実行結果をコールバックで返却。エージェントループがブロックしない | **採用**。長時間ツール（bash等）で有効 |
| **二面的ToolResult** | `ForLLM`（LLM向け簡潔テキスト）と `ForUser`（人間向けリッチ表示）を分離 | **採用**。UIの表示品質向上に直結 |
| **Heartbeat Subagent** | 定期タスクをサブエージェントとして自動スポーン | 参考。常駐型エージェントのCronスケジューラ設計に反映 |
| **BaseChannel** | 全チャネル共通のボイラープレートを基底型で吸収。10+チャネル実装 | **採用**。ChannelAdapterの設計に反映済み |
| **埋め込みワークスペーステンプレート** | `embed` ディレクティブでバイナリに初期テンプレートを同梱 | **採用**。セットアップウィザードで活用 |

### 7.2 Nanobot（Python, ~3,663 LOC core）

**概要**: Python製の軽量エージェント。MCP native対応。

**注目すべき設計パターン**:

| パターン | 内容 | 本プロジェクトへの反映 |
|----------|------|----------------------|
| **Providerレジストリ** | `ProviderSpec` データクラスでプロバイダを宣言的に登録。if-elif排除 | **採用**。Goではmap[string]ProviderFactory で実現 |
| **JSONLセッション形式** | append-only JSONL。各行が独立メッセージ。壊れにくく、diff可能。ツリー構造はparent_idフィールドで表現 | **採用確定**。セッションモデルの統一定義（§1と整合） |
| **2層メモリ** | MEMORY.md（キュレーション済み長期記憶）+ HISTORY.md（直近のコンテキスト要約） | **採用**。MEMORY.md + daily log の設計を強化 |
| **メモリ統合（consolidation）** | 古い会話をAIが要約してHISTORY.mdに統合。コンテキスト圧縮 | **採用**。セッション終了時に自動要約を生成 |
| **MCP native** | MCP Server/Clientを標準サポート。外部ツール連携の標準プロトコル | **採用**。Capability GatewayでMCPプロトコルを実装 |
| **Message Bus（Queue型）** | `asyncio.Queue` ベースの非同期キュー。購読パターンでチャネル連携 | PicoClawと同様のパターン。Bus設計に反映 |

### 7.3 ZeroClaw（Rust, ~31,500 LOC, 84ファイル）

**概要**: Rust製の極小フットプリント（3.4MB）AIエージェントランタイム。<5MB RAM、<10ms起動。963テスト。

**注目すべき設計パターン**:

| パターン | 内容 | 本プロジェクトへの反映 |
|----------|------|----------------------|
| **ハイブリッドメモリ検索** | Vector (cosine similarity) + Keyword (FTS5 BM25) を重み付き融合（v*0.7 + k*0.3） | **段階採用**。Phase 1: FTS5 BM25のみ（外部依存なし）。Phase 2: embedding追加（LLM APIまたはローカルモデルでベクトル生成）。コスト: embedding生成はAPIトークン消費あり |
| **8コアトレイト設計** | Provider, Channel, Memory, Tool, Observer, RuntimeAdapter, SecurityPolicy, Tunnel — すべてプラグイン可能 | **参考**。Goのinterface設計に反映。ただし8つは過剰 → 必要なものだけ |
| **チャネル許可リスト** | 空リスト = 全拒否（deny-by-default）。`"*"` で明示的オプトイン | **採用**。セキュリティデフォルトとして優秀 |
| **ChaCha20-Poly1305シークレット暗号化** | AEAD認証付き暗号でシークレット保存。XORレガシーからの自動マイグレーション | **採用**。APIキー等の保存に必須。Goでは `golang.org/x/crypto/chacha20poly1305` |
| **メモリハイジーン** | archive_after_days / purge_after_days による自動クリーンアップ。スロットル付き | **採用**。データ肥大化防止に有効 |
| **Gatewayペアリング** | 起動時6桁ワンタイムコード → bearer token交換。初回認証の安全な方法 | **採用（Phase 1の主認証方式）**。Tailscale統合（Phase 3）後はTailscale whois認証が主、ペアリングはフォールバック |
| **自律性レベル** | ReadOnly / Supervised / Full の3段階。設定で制御 | **採用**。Capability Gatewayの粒度制御に反映 |
| **Composio OAuth統合** | 1000+ SaaSアプリへのOAuthブリッジ | 参考。Phase 4以降で検討 |
| **Tunnel抽象化** | Cloudflare / Tailscale / ngrok / Custom をトレイトで統一 | **Phase 3で検討**。初期はTailscale直接利用。Tunnelインターフェースの抽象化は複数Tunnel対応が必要になった時点で導入 |
| **コマンドホワイトリスト** | shell実行時に許可コマンドのリストで制限 + 環境変数フィルタリング | **採用**。Landlock+seccompの補助レイヤー（Linux）、Windows環境では主要な制御手段。デフォルトホワイトリストを提供し、プロジェクト設定で拡張可能 |

### 7.4 Lobu（TypeScript, ~56,700 LOC, 205ファイル）

**概要**: TypeScript製のエンタープライズグレードAIエージェント・オーケストレーションプラットフォーム。Gateway + Orchestrator + Worker の3層アーキテクチャ。Slack/WhatsApp/Telegram対応。Kubernetes/Docker/ローカルデプロイ。

**注目すべき設計パターン**:

| パターン | 内容 | 本プロジェクトへの反映 |
|----------|------|----------------------|
| **SSE + HTTP ハイブリッド通信** | SSEでGateway→Workerにジョブ配信、HTTP POSTでWorker→Gatewayに応答返送。Worker側はstateless | **参考**。WebSocketの代替候補。reconnectに強い。ただし本プロジェクトではWebSocketの双方向性が必要なためWebSocket維持、SSEは通知用途で検討 |
| **InstructionProvider（優先度付き指示合成）** | `name`, `priority`, `getInstructions(context)` インターフェース。複数の指示源（プラットフォーム固有、ユーザー設定、スキル設定）を優先度順に合成してLLMに渡す | **採用**。エージェント定義のsystem_promptとオーガナイザーの指示を優先度付きで合成するメカニズムに活用 |
| **HTTP Proxyネットワーク隔離** | ワーカーの全HTTP/Sトラフィックを中央プロキシ経由に強制。ホスト名ホワイトリスト/ブラックリストで制御。デフォルト完全隔離 | **Phase 2で検討**。Landlock TCP制限の補完策。エージェント定義ファイルの`network_allowlist`をプロキシ設定に変換 |
| **Secret Proxy** | シークレット値をプレースホルダーに置換→Redis保存→ワーカー使用時に自動復号。エージェントプロセスへのシークレット露出を最小化 | **採用**。AEAD暗号化シークレットと組み合わせ。エージェントプロセスのメモリへのシークレット露出を最小化（Gateway側で動的置換） |
| **Scheduled Wakeup** | Cron式（`"0 9 * * 1"`）+ delay（一度のみ）+ maxIterations（最大実行回数）をBullMQジョブで実現。期限到来→ワーカーresume→タスク実行 | **採用**。常駐型エージェント定義の`cron`/`webhooks`フィールドの実装モデル。Goでは`robfig/cron`で実現 |
| **Scale-to-Zero** | アイドルタイムアウト（デフォルト30分）→Deployment削除→新メッセージ到着でPVC再マウント+`--continue`で自動再開 | **採用**。常駐型エージェントのリソース管理。アイドル時にgoroutine/メモリを解放、次回トリガーで状態を復元 |
| **PlatformAdapter** | `name`, `initialize()`, `start()`, `stop()`, `isHealthy()`, `getInstructionProvider()` | **参考**。ChannelAdapterに`isHealthy()`とInstructionProviderを追加する設計に反映 |
| **Module System** | HomeTab/Worker/Orchestrator/Dispatcher の4種モジュール。各モジュールがセッション開始/応答前/応答後のフックを実装 | **参考**。Phase 3以降のプラグイン拡張で検討。初期は不要 |
| **Session = Thread** | 各Slackスレッド/WhatsAppチャットが独立セッション。`user:channel:thread:conversationId`のキーで管理 | **参考**。WebChat UIでは各チャット画面 = セッション。将来のチャネルアダプタではスレッド = セッションのマッピング |

### 7.5 4プロジェクト横断比較

| 項目 | PicoClaw (Go) | Nanobot (Python) | ZeroClaw (Rust) | Lobu (TS) | 本プロジェクト |
|------|---------------|------------------|-----------------|-----------|---------------|
| 言語 | Go | Python | Rust | TypeScript | **Go** + TS |
| コード規模 | ~8K LOC | ~3.7K LOC | ~31.5K LOC | ~56.7K LOC | 目標: ~30-50K LOC（テスト含む） |
| バイナリ | シングル ~8MB | pip install | シングル ~3.4MB | bun + node_modules | シングル ~10-15MB |
| デプロイ | ローカル | ローカル | ローカル | **K8s/Docker/ローカル** | ホスト直接 |
| メモリ検索 | キーワード | キーワード+要約 | **ハイブリッド(Vector+FTS5)** | Redis KV | **ハイブリッド** |
| チャネル | 10+ | 9 | 9 | **Slack/WhatsApp/Telegram** | WebChat (Phase1) + アダプタ拡張 |
| マルチエージェント | サブエージェント | なし | なし | シングル（拡張予定） | **オーガナイザー + ユーザー定義Agent** |
| サンドボックス | アプリ層ガード | なし | アプリ層ガード | **HTTP Proxy + K8s隔離** | **Landlock + seccomp-bpf** |
| ネットワーク制御 | なし | なし | なし | **HTTP Proxyホワイトリスト** | Landlock TCP + プロキシ検討 |
| スケジューリング | Heartbeat | なし | なし | **Cron + Delay + maxIterations** | **Cron + Webhooks** |
| テスト | 少数 | 少数 | **963テスト** | ~9テストファイル | ZeroClawのテスト戦略を参考 |
| セッション | JSONL | JSONL | カテゴリ分類メモリ | **Redis + PVC** | **JSONL** |
| シークレット管理 | 環境変数 | 環境変数 | **AEAD暗号化** | **Secret Proxy + 暗号化** | **AEAD暗号化 + Proxy検討** |
| MCP対応 | なし | **ネイティブ** | なし | **OAuth Proxy** | **Capability Gateway** |
| ライセンス | Apache 2.0 | Apache 2.0 | MIT | Apache 2.0 | MIT（予定） |
| 最終コミット | 2025年5月 | 2025年4月 | 2025年5月 | 2025年5月 | — |
| メンテナ数 | 1人 | 1人 | 1人 | 3-5人 | 1人 |
| 既知脆弱性 | 報告なし | 報告なし | 報告なし | 報告なし | — |
| 依存更新頻度 | 月次 | 不定期 | 週次 | 月次 | — |

### 7.6 差別化分析と「フォーク vs 新規」判断

**問い**: 既存プロジェクトで代替できないか？車輪の再発明ではないか？

**回答**: 要素技術は既存プロジェクトに存在するが、**要件の組み合わせ**を満たす単体プロジェクトは存在しない。

| 要件 | PicoClaw | Nanobot | ZeroClaw | Lobu |
|------|----------|---------|----------|------|
| マルチエージェント（オーガナイザー+ユーザー定義） | ❌ | ❌ | ❌ | ❌ |
| カーネルレベルサンドボックス（Landlock+seccomp） | ❌ | ❌ | ❌ | ❌（K8s依存） |
| YAML定義エージェントフレームワーク | ❌ | ❌ | ❌ | ❌ |
| レスポンシブWebChat UI | ❌ | ❌ | ❌ | △（Slack等チャネル型） |
| 常駐型エージェント（Cron/Webhook） | △ | ❌ | ❌ | ✅ |
| ローカル直接実行+シングルバイナリ | ✅ | ❌ | ✅ | ❌（K8s前提） |

**フォーク最有力候補: PicoClaw**（Codex gpt-5.3-codex の推奨）
- Go製・軽量・シングルバイナリ・ローカル前提で土台が最も近い
- ただし、マルチエージェント層・サンドボックス層・WebChat UIが全て欠けており、フォークしてもコアの大半は新規実装が必要
- 実質的にはPicoClawの設計パターン（Message Bus、ToolResult、Provider等）を**参考にした新規開発**が最も現実的

**判断: 盆栽プロジェクトとして新規開発**
- 個人の学習・実験を兼ねたプロジェクト。フォークの効率性より、設計の自由度と理解の深さを重視
- 既存4プロジェクトの設計パターン（§12.7で15個採用）を活用し、車輪の再発明を最小化
- MVP（Phase 1）を小さく始め、段階的に育てる方針

### 7.7 調査結果の設計反映サマリー

4プロジェクトの調査で確認された、本プロジェクトに**確実に採用する**設計パターン（実装コスト概算・故障時デグレード戦略を含む）:

1. **Message Bus**（PicoClaw + Nanobot）: Gateway内のイベント配信基盤。Phase 1ではGo channelによる直接配信で十分。Phase 2のマルチエージェント導入時にBus抽象化を導入。実装コスト: ~800 LOC。故障時: Bus停止→Go channel直接配信にフォールバック
2. **二面的ToolResult**（PicoClaw）: `ForLLM` + `ForUser` の分離。UIリッチ表示とLLMコンテキスト節約を両立。実装コスト: ~200 LOC。故障時: ForUser生成失敗→ForLLMをフォールバック表示
3. **Providerレジストリ**（Nanobot）: map[string]ProviderFactory パターン。新プロバイダ追加時にコア変更不要
4. **メモリ検索**（ZeroClaw）: Phase 1はSQLite FTS5（BM25）のみ。Phase 2でembedding BLOB追加（ハイブリッド検索）。embedding生成はLLM API依存。実装コスト: Phase 1 ~400 LOC、Phase 2 +600 LOC。故障時: embedding生成失敗→BM25のみにフォールバック
5. **AEAD暗号化シークレット**（ZeroClaw）: ChaCha20-Poly1305でAPIキー等を安全に保存。実装コスト: ~500 LOC（鍵管理含む。§4.5参照）。故障時: 復号失敗→再設定ウィザード起動
6. **メモリハイジーン**（ZeroClaw）: 日数ベースの自動アーカイブ・パージ。データ肥大化防止
7. **チャネル許可リスト（deny-by-default）**（ZeroClaw）: セキュリティデフォルト
8. **メモリ統合（consolidation）**（Nanobot）: セッション終了時のAI要約生成
9. **Gatewayペアリング**（ZeroClaw）: ワンタイムコード → トークン交換の初回認証。Phase 1の主認証方式。Phase 3でTailscale whois追加後はフォールバック。実装コスト: ~400 LOC。故障時: コード期限切れ→再生成（TTL 5分）
10. **自律性レベル**（ZeroClaw）: ReadOnly / Supervised / Full の3段階制御。実装コスト: ~300 LOC。故障時: 判定不能→最も制限的なReadOnlyにフォールバック
11. **デグレード戦略・バックプレッシャー**（横断）: LLM API障害時はキューイング→リトライ（キュー長上限100、超過時は古いリクエストから破棄してユーザー通知）。DLQ: 3回失敗したリクエストはdead_letterテーブルに記録、手動再処理コマンドを提供。全体障害時: 読み取り専用モード（メモリ検索・セッション閲覧は可能、新規エージェント起動は停止）
12. **InstructionProvider（優先度付き指示合成）**（Lobu）: `name`, `priority`, `getInstructions(context)` インターフェース。エージェント定義のsystem_prompt、オーガナイザーの指示、プラットフォーム固有の指示を優先度順に合成。**Phase 1では固定的なsystem_prompt結合で十分。Phase 2でオーガナイザー導入時にインターフェース化**。実装コスト: ~300 LOC。故障時: 優先度ソート失敗→全指示を結合して渡す
13. **Secret Proxy**（Lobu）: シークレットの露出面を最小化する設計。エージェントプロセスにはプレースホルダーを渡し、Gateway側でHTTPリクエスト送信時にヘッダ/ボディを動的置換。エージェントプロセスのメモリダンプからのシークレット漏洩リスクを低減（ただし実行時にGatewayプロセス内では復号が必要）。AEAD暗号化と組み合わせ。**Phase 1ではAEAD暗号化のみ（環境変数経由）。Phase 2でProxy化**。実装コスト: ~400 LOC。故障時: 復号失敗→エージェントにシークレット未設定で起動（機能制限あり）
14. **Scale-to-Zero**（Lobu）: アイドルエージェントのgoroutine/メモリ解放。次回トリガー（Cron/Webhook/メッセージ）で状態復元。**Phase 2で導入**（Phase 1では常駐エージェント数が少ないためgoroutine保持で十分）。故障モード: 状態復元失敗→新規セッションとして起動（ユーザーに通知）。二重起動競合→mutex + SQLite行ロックで排他制御。状態不整合→セッションJSONLから最終状態を再構築。実装コスト: ~500 LOC
15. **Scheduled Wakeup**（Lobu）: Cron式 + delay（一度のみ）+ maxIterations。Goでは`robfig/cron`で実現。エージェント定義YAMLの`cron`/`webhooks`フィールドに直結。実装コスト: ~600 LOC（スケジューラ本体）+ ジョブ永続層（SQLiteにスケジュール状態・実行回数を記録。Gateway再起動時に復元）~200 LOC。delay/maxIterationsはPhase 2で段階導入。Phase 1ではCron式のみ。故障時: スケジュール未発火→次回サイクルでリカバリ

---

## 8. 外部レビュー結果（Codex gpt-5.3-codex）

### 8.1 ARCHITECTURAL CONSISTENCY（矛盾点）

| 指摘 | 該当行 | 深刻度 |
|------|--------|--------|
| タスク管理/ニュース配信AgentがUX設計(§2)では「常駐」前提なのに、§6/§10ではNice-to-Have/Phase4扱い | L47-50 vs L313-315, L492 | **高** |
| §3.2「TypeScriptを不採用」の見出しが、フロントエンドTS採用(L103,L457)と矛盾。「バックエンドで不採用」と書くべき | L110 vs L103, L457 | 中 |
| §1「JSONLツリー構造（ブランチ）」と§12.2「append-only JSONL（独立行）」でセッションモデル定義が二重化 | L18 vs L539 | **高** |
| Landlock TCP制限を前提(L133)にしつつ、Linux 5.13+を可用条件(L144)としている。TCP制御は6.7+が実質前提 | L133 vs L144 | 中 |
| Landlock+seccompがMust-Have(L302)だがWindows非対応(L231)。クロスプラットフォームの「Must」定義が曖昧 | L302 vs L231 | 中 |
| 外部チャネルはCut(L334)なのにプロジェクト構成にTelegram/Signalディレクトリがある(L444-449) | L334 vs L444 | 低 |
| §4.3が番号重複 | L162, L205 | 低 |

### 8.2 SECURITY GAPS（未対処の攻撃面）

| 指摘 | 深刻度 |
|------|--------|
| 「構造化メッセージ」はprompt injectionを本質的に防げない。JSONでも命令内容は汚染可能 | **高** |
| Capability証明書の発行者・署名・失効・TTL・再プレイ対策が未定義 | **高** |
| 「破壊的操作の人間承認」は小さな非破壊操作の連鎖で迂回可能 | **高** |
| 他エージェント会話の読み取り可能設計が横展開面（機密プロンプト/トークン漏えい導線） | **高** |
| 共有ワークスペースのファイル汚染によるクロスエージェント感染。整合性検証なし | 中 |
| MCPサーバ自体の信頼境界・署名配布・サプライチェーン検証が未記載 | 中 |
| 「不変ログ」が宣言のみ。WORM/外部追記専用ストア/改ざん検知がない | 中 |
| cgroups制限にPID上限・I/O制限が未定義。fork爆弾/IO枯渇未対策 | 中 |
| 「十分な分離」は過大主張。Landlock+seccompは強化策であって完全分離ではない | 中 |
| ネットワーク境界が薄い。mTLS、証明書管理、WS再接続時のトークン更新が欠落 | 中 |
| AEAD暗号化の鍵管理（KMS/OS keyring/ローテーション/復旧）が未定義 | 中 |
| Windows方針が「bash禁止」に寄りすぎ。PowerShell/MCP経由のシェル実行リスク未整理 | 中 |

### 8.3 FEASIBILITY（現実性）

- 15-20K LOC目標は機能範囲に対して楽観的すぎる
- ZeroClawの963テストを称賛しているのに、自プロジェクトのテスト規模目標がない
- **現実的見積り**: 最小プロトタイプなら20K未満。実運用品質なら30K-50K+（テスト除く）

### 8.4 MISSING CONCERNS（重要論点の欠落）

| 欠落項目 | 説明 |
|----------|------|
| エラーハンドリング方針 | 再試行、冪等性、部分失敗、タイムアウト階層が未定義 |
| 可観測性 | メトリクス、分散トレース、相関ID、SLO、アラート設計が未記載 |
| マイグレーション戦略 | DB/設定/セッション形式のバージョン管理が未定義 |
| テスト戦略 | 単体/統合/E2E/負荷/セキュリティテストの境界が不明 |
| CI/CD | lint、静的解析、脆弱性スキャン、SBOM、署名配布が未定義 |
| デプロイ自動化 | 更新・ロールバック・秘密配布が空白 |
| ログ方針 | PIIマスキング、保持期間、監査ログとの分離が不明 |
| LLM APIレート制限 | 同時実行制御・429対処がない |
| コスト管理 | モデル別予算、トークン上限、日次上限、異常検知が未定義 |
| モデル評価/Evals | オーガナイザー品質や自律動作の回帰検知不能 |

### 8.5 OVER-ENGINEERING（個人用途に対する過剰設計の指摘）

- 9層防御を最初から全実装は過重
- Message Bus、BaseChannel、Tunnel抽象化まで初期で抱えるのは設計先行が強すぎる
- 毎時+操作前スナップショット+Gitは運用負担とストレージ管理が重い
- ペアリング/Tailscaleトークン/Cloudflareの並列は認証系の複雑化
- ハイブリッド検索は初期はBM25単体で十分。ベクトル融合は後回し可能

### 8.6 Section 7の妥当性

- 採用理由が「採用」の一言だけで、実装コスト・故障モード・運用負担の定量根拠がない
- L539「JONL」は誤記（JSONL）
- 「外部依存なし」は不正確。embedding生成はモデル依存
- コマンドホワイトリストを「参考」に留めたのは悪手。Landlock補助として採用優先度が高い
- テストアーキテクチャ（fixture、golden、fuzz、replay）を取り込んでいない
- 「障害時のデグレード戦略」「キューのバックプレッシャー」「DLQ/再処理」パターンが抜けている
- ペアリング認証とTailscale ID認証の関係整理が不足。認証フローが二重化

### 8.7 総評

> 設計の方向性は悪くないが、現状は「強い言葉で武装した未確定設計」。最も深刻なのは、UX要件とロードマップの矛盾、そしてセキュリティ対策の"仕組み名列挙"止まり。実装前に、要件縮小・脅威モデル再定義・運用設計（監視/テスト/更新）を先に固定しないと失敗する。

> **Rev.4対応状況**: 上記指摘の大半をRev.4で対応。アーキテクチャ矛盾(§1,§2,§3,§4番号修正)、セキュリティ詳細設計(§4.4改訂,§4.5新設)、過剰設計の簡素化(フェーズ分け)、Section 12修正(誤記,採用理由,格上げ)、運用設計(§14新設)。一部残課題あり（下記Rev.5レビューで指摘）。

> **Rev.5追加対応**: エージェント設計をフレームワーク化（§2.1, §2.5）。タスク管理/ニュース配信Agentは「例示」に変更し、ユーザーが自由に作成する設計に。Cron/WebhooksをPhase 1に昇格。OS優先順位策定（Linux Tier 1 → macOS Tier 2 → Windows Tier 3）、macOSサンドボックス戦略（§4.2.1）追加。DinD制約をアーキテクチャ原則に明記。

### 8.8 Rev.5レビュー結果（Codex gpt-5.3-codex、2回目）

| カテゴリ | 指摘 | 深刻度 | Rev.6対応 |
|----------|------|--------|-----------|
| **ARCHITECTURAL CONSISTENCY** | 監査ログが§4.3でPhase 2、§10でPhase 3と不整合 | **高** | **修正済み**: §10をPhase 2に統一 |
| 同上 | Must-HaveにOS別サンドボックスがあるがWindows=Phase 4。Must-Haveと実装時期の語義が揺れている | **高** | **修正済み**: Must-Haveテーブルに実装Phaseカラム追加。「全Phase通じて最終的に必須」と定義明確化 |
| 同上 | DB選定が`mattn/go-sqlite3 or modernc.org/sqlite`で未確定。シングルバイナリ方針との整合が弱い | 中 | **修正済み**: modernc.org/sqliteに確定。CGO不要でシングルバイナリ方針と整合 |
| **SECURITY GAPS** | WebhookをPhase 1必須に上げたが署名検証・リプレイ防止・送信元認証が未設計 | **高** | **修正済み**: §4.5にWebhookセキュリティ追加（HMAC-SHA256署名、タイムスタンプ+nonce、鍵ローテーション）。Webhook署名検証はPhase 2に移動 |
| 同上 | Capability証明書の失効リストがインメモリのみ。再起動時整合性、鍵ローテーション、audience束縛が未定義 | **高** | **修正済み**: §4.4にSQLite永続化、audience束縛、署名鍵ローテーション追記 |
| 同上 | macOSプロファイル`*:443`が「LLM API接続先のみ」方針より広い | 中 | **修正済み**: §4.2.1のプロファイル例をホスト限定に変更 |
| 同上 | iptablesフォールバックがroot不要方針と衝突 | 中 | **修正済み**: アプリ層DialContext制限に変更。iptables非推奨と明記 |
| 同上 | Secret Proxyの「平文を残さない」は過大主張 | 中 | **修正済み**: 「露出面を最小化」に表現変更。Gateway内復号の必要性を明記 |
| **FEASIBILITY** | Phase 1 ~10K LOCに対してスコープが過重 | **高** | **修正済み**: ~12-15K LOCに上方修正。UIウィザードとWebhook署名をPhase 2に後回し |
| 同上 | Scheduled Wakeupの実装難度がジョブ永続層含めて高い | 中 | **修正済み**: Phase 1はCron式のみ。delay/maxIterationsはPhase 2。ジョブ永続層（SQLite）の追加LOC注記 |
| 同上 | testcontainers-go（SQLite）は過剰 | 低 | **修正済み**: インメモリSQLite(`:memory:`)に変更 |
| **MISSING CONCERNS** | Webhook運用（認証鍵配布、再送ポリシー）未定義 | 中 | **修正済み**: §4.5にWebhookセキュリティ全体設計追加 |
| 同上 | SLO/アラート閾値/runbook不足 | 中 | **修正済み**: §14.2にアラート閾値追加（個人用途のためSLOは不要と明記） |
| 同上 | リリースアーティファクト署名・provenance欠落 | 中 | **修正済み**: §14.5にcosign署名追加 |
| 同上 | セキュリティテスト頻度・合格基準未定義 | 中 | **修正済み**: §14.4にセキュリティテスト基準追加 |
| **OVER-ENGINEERING** | ChannelAdapterをPhase 1から構造化は過剰 | 中 | **修正済み**: §7にPhase 1はWebChat直接実装の注記追加 |
| 同上 | InstructionProvider/Secret Proxy/Scale-to-ZeroをMVPで全採用は過剰 | 中 | **修正済み**: §12.7の各項目にPhase分け注記追加 |
| **SIMILAR PROJECT ANALYSIS** | 比較軸にライセンス・保守継続性等の実務軸が不足 | 中 | **修正済み**: §12.5にライセンス/最終コミット/メンテナ数/既知脆弱性/依存更新頻度行を追加 |
| 同上 | Scale-to-Zeroのローカルgoroutine移植の故障モード評価不足 | 中 | **修正済み**: §12.7に二重起動競合・状態不整合の故障モード追記 |
| **PREVIOUS REVIEW** | 「全項目対応済み」は過大 | 中 | **修正済み**: 「大半を対応」に変更。残課題を明示 |

> **Rev.5レビュー総評**: Rev.4/Rev.5で設計の骨格は大幅に改善。Rev.5レビューの指摘は「詳細の詰め不足」が中心。
>
> **対応状況の正直な評価**: 上記テーブルの「修正済み」は**設計文書に追記した**ことを意味し、**リスクが解消された証拠（テスト結果等）**ではない。実際の検証は実装Phase（§10 DoD参照）で行う。設計段階で「修正済み」と断言するのは過大主張であるため、より正確には「**設計対応済み・実装未検証**」と読み替えるべき。

### 8.9 差別化レビュー結果（Codex gpt-5.3-codex、3回目）

**問い**: 既存プロジェクトとの優位性は？車輪の再発明にならないか？

| 項目 | Codex判断 |
|------|-----------|
| 新規開発の正当性 | 「ゼロから新規」は弱い。要素技術は既存にある。ただし要件の**組み合わせ**を満たす単体プロジェクトは不在 |
| 差別化の価値 | マルチエージェント+カーネル隔離: **高**。YAML定義: **中**（追加実装で可能）。シングルバイナリ: **高** |
| フォーク最有力候補 | **PicoClaw**（Go製・軽量・ローカル前提で土台が最も近い） |
| フォーク vs 新規 | PicoClawフォーク: 中コスト・中リスク。完全新規: 高コスト・高リスク |
| 過剰な独自性 | 初期からの抽象化・認証多重化が過剰。独自性自体でなく**導入タイミングが早すぎる**のが問題 |
| **最終判断** | **「作るべきか？」にはYes。ただし「ゼロから」ではなく「PicoClawフォークで作るべき」** |

> **対応方針**: 本プロジェクトは個人の盆栽プロジェクト（学習・実験目的）として新規開発を選択。フォークの効率性より設計の自由度と理解の深さを重視。ただしCodex指摘の「車輪の再発明リスク」は§12.7の15パターン採用で最小化する。§12.6に判断根拠を記載済み。
>
> **この判断の検証条件**: Phase 1完了時点で「PicoClawフォークの方が早かった」と判断した場合、Phase 2以降でフォークへの切り替えを検討する。判断基準: Phase 1が3ヶ月（見積り）を2倍以上超過した場合。

### 8.10 Rev.6レビュー結果（Codex gpt-5.3-codex、4回目）

**総評**: 「発想は強いが、設計文書としては未成熟。§13.8/§13.9の自己評価が楽観的すぎる」

| # | カテゴリ | 指摘 | 深刻度 | Rev.7対応 |
|---|----------|------|--------|-----------|
| 1 | 整合性 | 文字化けが混在（※stdinエンコーディング問題。ドキュメント自体は正常UTF-8） | Critical | **対象外**: ドキュメント自体は正常。Codexへのstdin渡し時の文字コード問題 |
| 2 | セキュリティ | 脅威モデルがない（攻撃者像・資産・経路・優先順位） | Critical | **対応済み**: §4.0に軽量脅威モデル新設（資産5項目・攻撃者3類型・攻撃経路8パス） |
| 3 | 実現性 | Phase 1スコープが個人開発として過大（~12-15K LOC） | Critical | **対応済み**: Phase 1を~6-8K LOCに半減。Linux only/WebChat only/2ツール/単一プロバイダ |
| 4 | セキュリティ | Prompt injection対策が過大主張（「防げる」設計） | Critical | **対応済み**: §4.4を「低減（mitigate）」に修正。人間承認を最終防御線と明記 |
| 5 | 前回レビュー | §13.9の結論が時期尚早（未解決項目を残したままGo判断） | Critical | **対応済み**: §13.8総評を「設計対応済み・実装未検証」に修正。§13.9に検証条件追加 |
| 6 | 整合性 | Must-Haveと実装優先度が混同 | High | **対応済み**: §6にPhase 1 MVP列を追加。「Must-Have ≠ Phase 1」を明記 |
| 7 | セキュリティ | Capability Token運用要件不足 | High | **後回し**: Phase 3（マルチエージェント）で詳細化。Phase 1では不要 |
| 8 | セキュリティ | Secret管理の鍵ライフサイクルが薄い | High | **後回し**: Phase 2以降で詳細化。Phase 1はOS keyring + Argon2id最小構成 |
| 9 | 実現性 | 3 OS同時設計が早すぎる | High | **対応済み**: Phase 1はLinux onlyに限定。macOSはPhase 2、WindowsはPhase 4 |
| 10 | 過剰設計 | 9層防御等の早期導入 | High | **対応済み**: Phase 1はL1-L5のみ。L6-L9はPhase 2-4に分散 |
| 11 | 不足論点 | 受け入れ基準（DoD）不足 | High | **対応済み**: §10にPhase別DoD追加（定量指標付き） |
| 12 | 類似PJ分析 | 失敗要因比較が弱い | High | **後回し**: 盆栽プロジェクトのため、実装しながら発見するアプローチ |
| 13 | 整合性 | データ層責務の重複 | Medium | **後回し**: Phase 1実装時にデータフロー整理 |
| 14 | 不足論点 | バックアップ復元訓練が弱い | Medium | **後回し**: Phase 1はgit自動コミットで十分 |
| 15 | セキュリティ | Webhook正規化ルール曖昧 | Medium | **後回し**: WebhookはPhase 3。Phase 1では不要 |
| 16 | 過剰設計 | チャネル抽象化の早期過剰設計 | Medium | **既対応**: §7にPhase 1はWebChat直接実装の注記あり（Rev.6で対応済み） |
| 17 | 前回レビュー | §13.8の「修正済み」に検証証跡なし | Medium | **対応済み**: §13.8総評を「設計対応済み・実装未検証」に修正。DoD導入で実装時に検証 |
| 18 | 類似PJ分析 | §12.6比較の公平性に欠ける | Medium | **後回し**: 盆栽プロジェクトのため比較の厳密性より実装優先 |

### 8.11 Codex戦略アドバイス（Rev.6レビュー後）

**結論**: 「設計書を最小限に絞って直し、すぐ実装に入る」

**採用した修正**:
1. 軽量脅威モデル追加（§4.0） — 1ページ、資産/攻撃者/経路/優先度
2. Phase 1再定義 — Linux/WebChat/2 tools/単一provider、~6-8K LOC
3. Prompt Injection表現の現実化 — 「防げる」→「低減する」
4. DoDを数値で明記 — Phase別に定量指標
5. §13.8/§13.9を自己評価から証拠志向へ修正
