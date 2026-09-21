# ADR-0007: カタログ外の補助モデルについて `models pull` に kind/arch/trigger の上書きを許す

- Status: Accepted
- Date: 2026-07-14
- Supplements: [ADR-0006](0006-lora-controlnet-registry.ja.md)

## Context

ADR-0006 は LoRA と ControlNet をレジストリの一級 kind にし、キュレーション済みのものを
`models pull <name>` で入手できるようにした。しかし kind の判定を**カタログだけ**に結線
していた。`modelsPull` は `kind`・`arch`・trigger words を一致したカタログエントリから
読む（`e.Kind`・`e.Arch`・`e.TriggerWords`）。参照がカタログ名**でない**とき — 生の
`hf:owner/repo/file`・`civitai:<id>`・直 URL — は `!known` 分岐に落ち、そこは次のように
ハードコードされている:

```go
prof = profile.ArchDefaults(profile.Detect(regName))  // diffusion の既定値をフルで
prof.Name = regName
rating = profile.RatingSafe
// kind は "" (KindDiffusion) のまま。trigger words も無し
```

つまり、キュレーション済みカタログに無い LoRA を pull すると **ベースの diffusion
モデル**として登録される: `Kind` が間違い、持つ必要のないフィールド（prediction・
clip-skip・VAE・hires 系）まで入った diffusion プロファイルが付き、trigger words は無い。
その帰結は ADR-0006 自身が kind の混同について警告していた内容と一致する:

- `--lora <name>` がそれを解決できない — `resolveAuxModel` は誤った kind で登録された
  名前を拒否する（"not a LoRA"）。
- `models list`（および `--json` を読むフロントエンド）が、それを描画可能なベースモデルと
  して表示する。ADR-0006 が導入した `MultiComponent` ヒューリスティックと arch フィルタは、
  このエントリに対しては無効化される。
- arch が記録されていないので、非互換ベースのフィルタが発火できない。

`import` はまさにこの理由で既に `--kind/--arch/--trigger` を持っているが、登録できるのは
**ローカル**ファイルだけである。今日の唯一の回復手段は二段構えの踊りだ: 参照を `pull` し
（誤登録される）、続いてダウンロード済みのファイルを `--kind` 付きで `import` して
エントリを上書きする。そこが欠落である: **取得してタグ付けする経路がローカルファイルには
あるのに、リモート参照には無い。**

## Decision

**`models pull` に、`models import` が持つのと同じ `--kind`・`--arch`・`--trigger` の
上書きを与える。ただし適用するのはカタログ外（`!known`）の経路だけとする。** カタログ
エントリは ADR-0006 §3 が意図したとおり、権威のままである。

1. **`pull` に新しいフラグ** — `--kind lora|controlnet|upscaler`（既定: ベース diffusion、
   従来どおり）、`--arch sdxl|sd15|…`（既定: レジストリ名からの自動判定、従来どおり）、
   `--trigger "a,b"`（カンマ区切りの起動トークン）。検証ヘルパは `import` と同じ:
   `normalizeKind`・`splitTriggers`。

2. **kind→プロファイルの対応付けが 1 つの共有された純粋関数になる。** ADR-0006 の規則 —
   diffusion はフルの `ArchDefaults`、アップスケーラは素の `Profile{Name}`、LoRA/ControlNet は
   `Profile{Name, Arch}` だけでそれ以外は持たない — は `import` にインライン展開されていた。
   これを `auxProfile(kind, name, arch)` に抽出し、`pull` と `import` の**両方**から使う。
   これにより invariant がちょうど 1 箇所に住み、直接ユニットテストできる（テスト容易性の
   規則どおり）。補助 kind はどちらの経路でも VAE を持たない（従来どおり）。

3. **カタログエントリは上書きを無視し、そう言う。** 参照がカタログエントリに一致し、かつ
   上書きフラグが渡されたとき、`pull` は上書きを無視したという注記を stderr に出す
   （カタログはキュレーション済みで権威だからである）。黙って適用すれば利用者が検証済み
   モデルに誤ったタグを付けられてしまう。黙って捨てれば意図を隠すことになる。見える形の
   注記が誠実な中間である。

4. **既定は変わらず、後方互換である。** フラグ無しなら、カタログ外の参照は従来どおり自動
   判定された arch を持つベース diffusion モデルとして登録される — 既存の `pull` 呼び出しは
   すべて同一に振る舞う。

### 対象外

- **`pull` の `--vae`。** `import` はローカルのベースモデル用にこれを持っている。`pull` の
  VAE はカタログエントリから来るものであり、別の VAE を必要とする生のベースモデル pull は
  報告された問題ではない。意図的に追加しない — 要望が出たら再考する。
- **形式/効果の検証。** ADR-0006 と `docs/*/adding-a-model.md` は、LoRA を*カタログに
  入れる前に*検証すること（kohya キー、描画して効果があること）を要求している。生の参照を
  `--kind lora` で pull する利用者は、自分のマシンに関してそのキュレーションをオプトアウト
  している。`pull` はテンソルの検証を試みない。

## Consequences

- `image-forge models pull hf:owner/repo/lora.safetensors --kind lora --arch sdxl
  --trigger "…" --name my-lora` が、正しい型・arch 束縛・trigger 付きの LoRA を一手で
  登録する。その後 `--lora my-lora:0.8` が解決する。
- 二段構えの `pull` → `import` 回避策は不要になる（引き続き動作はする）。
- kind→プロファイルの invariant に単一の住所（`auxProfile`）ができ、`import` と `pull` が
  乖離できなくなり、対応付けはネットワークに触れずにユニットテストできる。
- 上書きが尊重されるのはカタログ外のときだけである。キュレーション済みエントリがフラグで
  再タグ付けされることは決してない — ADR-0006 がカタログの上に築いた arch 互換性の保証は
  そのまま保たれる。
- 上書きは ADR-0006 の arch と同じ意味で助言的である: image-forge は利用者が主張した内容を
  記録し、描画時の最終的な裁定者は sd.cpp のままである。誤ったラベルの `--kind` は、
  黙った誤描画ではなく明快な解決/互換エラーを生む。
