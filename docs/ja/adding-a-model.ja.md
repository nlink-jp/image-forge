# カタログへのモデル追加

キュレーション済みカタログは [`internal/catalog/catalog.go`](../../internal/catalog/catalog.go)
の `Default()` が返すリストです。各エントリにより利用者は
`image-forge models pull <name>` でチェックポイントを取得し、**モデルごとの落とし穴**
（CLIP-skip・専用VAE・プロンプト前置き 等）がプロファイル経由で自動適用されます。本書は
その追加手順のチェックリストです。

> カタログの目的は落とし穴の隠蔽です。良い出力に非自明な設定が要るなら、それをエントリに
> encode してください — 利用者に発見させない。

## 0. 事前準備

- 正式チェックアウト（`util-series/image-forge`）の `main` で作業する。
- モデルの実ソース参照が必要。Civitai の場合、E2E ダウンロードに `CIVITAI_TOKEN` が要る。

## 1. ソースを特定し、正確な参照を得る

`Source` は Hugging Face / Civitai / 直 URL / 多コンポーネントに対応。

### Hugging Face（単一ファイル）— 可能ならこれが最良

**ファイル修飾**参照を使う: `owner/repo/file.safetensors`。

```sh
# repo ルートの .safetensors と gated 状態を確認:
curl -s "https://huggingface.co/api/models/SG161222/RealVisXL_V5.0" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); print("gated:",d.get("gated"),"license:",(d.get("cardData") or {}).get("license")); [print(s["rfilename"]) for s in d["siblings"] if s["rfilename"].endswith(".safetensors") and "/" not in s["rfilename"]]'
```

- **Gated repo**: `401`＝トークン無し/不正、`403`＝トークンは有効だがWebでライセンス未承諾。
  `403` は利用者にライセンス承諾を強いる代わりに **ungated ミラー** を探す
  （例 `camenduru/FLUX.1-dev/ae.safetensors`、`adamo1139/…-ungated/…`）。
- **diffusers 形式の repo は単一ファイルpull不可。** `.safetensors` が `unet/` /
  `text_encoder/` / `vae/` 配下のみでルートに単一チェックポイントが無い場合、単一 `HF`
  参照は使えない → Civitai 版ID か多コンポーネントを使う。（`John6666/*` の Civitai
  ミラーの多くは diffusers 形式。）
- **HF Xet ストレージ**: 一部 repo は Xet 経由（`resolve/main/…` が `*.cdn.hf.co` へ
  302 リダイレクト）。リダイレクト追従の全体 GET は実バイトを返し、我々の DL 実装は対応
  済み。`Range: 0-0` プローブが1バイトでなく小さなマニフェストを返しても問題ない。

### Civitai — モデルIDではなく VERSION ID を使う

`Source.Civitai` は **version ID**（`models pull` が API で解決する数字）。カタログURLは
たいてい *モデル* ID しか無いので version を引く:

```sh
# model id 439889 -> 最新 version id + base model + primary file
curl -s "https://civitai.com/api/v1/models/439889" \
  | python3 -c 'import sys,json; d=json.load(sys.stdin); v=d["modelVersions"][0]; print("version id:",v["id"],"| base:",v["baseModel"]); [print(f["name"],f.get("type")) for f in v["files"] if f.get("primary")]'
```

または version 個別 URL の `?modelVersionId=` を読む。DL には `CIVITAI_TOKEN`（env か
config）が要る。トークンはログから秘匿される — **絶対にコミットしない**。

## 2. プロファイル項目を決める

アーキ既定から始め、落とし穴だけ上書きする。

| 項目 | 選び方 |
| --- | --- |
| `Arch` | `ArchSDXL` / `ArchSD15` / `ArchSD35` / `ArchFlux` / `ArchZImage`。Pony・Illustrious は `ArchSDXL`。 |
| `Prediction` | ほぼ `PredEps`。v-prediction モデル（NoobAI v-pred 等）は `PredVPred`。 |
| `Rating` | `RatingSafe` / `RatingQuestionable` / `RatingExplicit`。NSFW可のアニメ/Pony → `Questionable`、NSFW寄り → `Explicit`。`Questionable`/`Explicit` は `--allow-nsfw` 必須。フラグがゲート、ratingは正直な信号。判断は利用者に委ねる。 |
| `License` | 上流ライセンス。コミュニティマージで不明瞭なら `(verify)` / "see Civitai listing" を付す。 |
| `MinRAMGB` / `RecRAMGB` | SDXL: `16` / `32`。SD1.5: `8` / `16`。FLUX/SD3.5-large: `16`（Q4）/ `32`。 |

### encode すべきモデル別の落とし穴

- **SDXL 専用 VAE** — SDXL エントリには必ず fp16-fix VAE を付与（fp16 の黒画像NaN対策）:
  `VAE: "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors"`。
- **CLIP-skip** — SDXL arch 既定は **2**（アニメ寄り）。アニメ/Pony/Illustrious はそのまま
  （明示するなら `ClipSkip: 2`）。**実写系SDXL**（RealVisXL・Juggernaut）は `ClipSkip: 1`
  に上書き。
- **Pony スコアタグ** — Pony 系は品質前置きが要るので隠す:
  `PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up"`。
- **hires.fix** — 上流注記が「必ず hires」を推奨するモデルは既定ONにする:
  `HiresEnabled: true`（プロファイルは `HiresScale` / `HiresDenoise` / `HiresUpscaler`
  も尊重。0/"" のままなら控えめ既定 1.5 / 0.5 / latent）。利用者は `--hires auto|on|off`
  で上書き可。
- **アップスケーラ(ESRGAN)エントリ**は別の `Kind: "upscaler"` — VAE/prediction/profile
  無しの単一 `.pth`/`.safetensors` ESRGAN（例 `realesrgan-x4plus`）。`image-forge upscale`
  と hires `--hires-upscaler model` で使う。アップスケーラは**アーキテクチャ非依存**なので
  `Arch` は未設定のままにする。
- **LoRA エントリ**は `Kind: catalog.KindLoRA`。アップスケーラと違い **`Arch` 必須** —
  LoRA は学習元アーキテクチャでしか動かず、レジストリの `arch` があるからこそ
  `models list --json` の利用者（GUI 含む）が非互換なものを隠せる。VAE / prediction /
  clip-skip / hires 系フィールドは持たない。

  ```go
  {
      Name: "lcm-lora-sdxl", Kind: KindLoRA, Arch: profile.ArchSDXL,
      Rating: profile.RatingSafe, License: "OpenRAIL++",
      MinRAMGB: 16, RecRAMGB: 32,
      Source: Source{HF: "latent-consistency/lcm-lora-sdxl/pytorch_lora_weights.safetensors"},
      Notes:  "Few-step sampling. Use ~4-8 steps, CFG ~1-2, sampler lcm.",
      // TriggerWords: []string{"mythp0rt"},  // 起動トークンが必要な LoRA の場合
  }
  ```

  **`TriggerWords`** は LoRA を有効化するためプロンプトに入れるトークン（Civitai の
  "trained words"）。配布元に記載があれば必ず設定する — **トリガーが欠けた LoRA は
  エラーも出さずロードされ、黙って何もしない**（デバッグが非常に辛い）。インストール時に
  レジストリへ複写され、`pull` 完了時に表示され、`models list --json` で `trigger_words`
  として公開される。トリガー不要な LoRA（LCM、スライダー系）は空のままでよい。

- **`LicenseFlags`** は特記すべき利用制約を機械可読な識別子で記録する
  （`catalog.LicenseNonCommercial` / `LicenseNoDerivatives` / `LicenseAttribution` /
  `LicenseShareAlike` / `LicenseReview`）。フロントエンドが強調表示できるようにするため —
  フリーテキストの `License` だけでは表記ゆれで確実に解析できない。**全 kind に適用する**
  （特にベースモデルは商用可否が重要）。**推測せず配布元から導出する**。ライセンスが
  不明・未宣言なら `LicenseReview` を使う。
  Civitai なら API の `allowCommercialUse` / `allowDerivatives` / `allowNoCredit` から:

  ```sh
  # 生成画像の商用利用は "Image"(または"Sell") が必要。rent のみ => non-commercial
  curl -s "https://civitai.com/api/v1/models/<id>" | python3 -c "
  import sys,json; m=json.load(sys.stdin)
  acu=str(m.get('allowCommercialUse')); print('non-commercial:', not ('Image' in acu or 'Sell' in acu))
  print('no-derivatives:', m.get('allowDerivatives') is False)
  print('attribution:', m.get('allowNoCredit') is False)"
  ```

  これらは参考情報であり法的助言ではない。レジストリへ複写され、`models list --json` の
  `license_flags` として公開される。寛容なモデル（OpenRAIL / Apache / 商用可の Civitai）は
  空のままでよい。

  **エントリ追加前に形式を検証すること。** sd.cpp は kohya 形式のキー
  （`lora_unet_*.lora_down.weight` / `.lora_up.weight` / `.alpha`）を期待する。
  safetensors のヘッダだけ読めば判定でき、全体をダウンロードする必要はない:

  ```sh
  python3 -c "
  import struct, json, sys
  with open(sys.argv[1],'rb') as f:
      hdr = json.loads(f.read(struct.unpack('<Q', f.read(8))[0]))
  keys = [k for k in hdr if k != '__metadata__']
  print('kohya形式:', any(k.startswith('lora_unet') for k in keys), '| テンソル数:', len(keys))
  print('text encoder テンソル有:', any(k.startswith('lora_te') for k in keys))
  " some-lora.safetensors
  ```

  そのうえで**実際に1枚生成**し（`gen --lora <name>:1.0`）、同一 seed で LoRA 無しと
  比較する。ロードできても効果が無いもの、sd.cpp がクラッシュするものはカタログに
  載せない。（`lora_te*` を持たない UNet 専用 LoRA は sd.cpp の事前マージ経路で
  SIGSEGV していた。image-forge は `lora_apply_mode = at_runtime` に固定して回避。
  ADR-0006 参照。）

- **ControlNet エントリ**は `Kind: catalog.KindControlNet`。同じく **`Arch` 必須**。
  現時点で同梱エントリはゼロ — sd.cpp が受け付ける ControlNet 形式が限定的で、
  **実際に生成できていないものはカタログに載せない**方針のため。検証が済むまでは
  `models import <path> --kind controlnet` でローカルファイルを登録する。なお
  ControlNet の変更は**ベースモデルの再ロードを伴う**（エンジンの reload key に含まれる）。
  レンダごとに適用される LoRA とはコストが違う点に注意。
- **多コンポーネント**（FLUX / SD3.5 / Z-Image / **Anima**）— `HF`/`Civitai` を空にし、
  `DiffusionModel` ＋エンコーダ（`ClipL` / `ClipG` / `T5XXL` / `LLM`）＋ `VAE` を設定。
  Civitai の「チェックポイント」は **DiT のみ**であることが多い。safetensors ヘッダを見て
  `model.diffusion_model.*` 以外が入っているか確認すること。入っていなければ text encoder と
  VAE を別途指定しないと sd.cpp は `failed to load model` で落ちる。
  （Anima: DiT + Qwen3-0.6B + Qwen-Image VAE）
- **アーキ名が互いの部分文字列になっている場合に注意。** `profile.Detect` はモデル名で
  判定するが、`animagine`（SDXL）は `anima`（別アーキ）を含む。長い名前が先にマッチする
  よう順序を組み、テストで固定すること。誤判定すると**黙って別のプロファイル既定値**が
  適用される。**標準 fp8
  （`t5xxl_fp8_e4m3fn`）・bf16・GGUF のみ使う** — ComfyUI の `fp8_scaled` / `fp8_mixed`
  ビルドは sd.cpp 非互換（ブランク/ロード失敗）。

## 3. エントリを追加

`Default()` のリストに、類似モデルと同じ場所へ追加。例（Civitai Pony）— 実エントリは
`prefect-pony-xl` / `realvisxl-v5` 付近参照:

```go
{
    Name: "prefect-pony-xl", Arch: profile.ArchSDXL, Prediction: profile.PredEps,
    Rating: profile.RatingQuestionable, License: "Pony-derived; see Civitai listing",
    MinRAMGB: 16, RecRAMGB: 32,
    Source: Source{
        Civitai: "2114187", // https://civitai.com/models/439889 (v6)
        VAE:     "madebyollin/sdxl-vae-fp16-fix/sdxl.vae.safetensors",
    },
    ClipSkip:     2,
    PromptPrefix: "score_9, score_8_up, score_7_up, score_6_up, score_5_up, score_4_up",
    Notes:        "Prefect Pony XL v6 (Civitai): high-quality Pony SDXL. Needs CIVITAI_TOKEN.",
},
```

## 4. テスト追加/更新

テストは必須。[`internal/catalog/catalog_test.go`](../../internal/catalog/catalog_test.go) で:

- 既存のテーブルテスト（Pony score-prefix、Civitai version-id、実写 clip-skip-1 等）に名前を追加。
- 基本 invariant（名前非空・一意・license有・prediction伝搬）は全エントリを既にカバー。緑を保つ。

## 5. ビルド & 確認

```sh
make build          # スキャフォールドがコンパイルできる
make test           # go test（third_party除外）
make vet
./dist/image-forge models list --catalog   # エントリが出る
```

## 6. 実エンジンでE2E検証（リリース前必須）

メタデータが正しいだけでは不十分 — pull して描画する:

```sh
make build-engine
image-forge models pull <name> [--allow-nsfw]     # DL（既存ファイルは再利用）
image-forge gen -m <name> -p "…" -o /tmp/test.png
```

そして **PNG を開いて**、黒画像（VAE/NaN）でもノイズ（予測方式ミス）でもない、まともな画像
であることを確認。`models pull` は既存チェックポイント/VAE を再利用（`haveFile`）するので、
ファイルが手元にあれば再検証は安い。

## 7. 出荷

- `CHANGELOG.md` を更新（単一モデル追加は patch bump）。
- README はモデルを列挙しないので通常変更不要。
- 組織チェックリストに従いリリース: `chore: release vX.Y.Z` → tag → `make package`
  （署名 + notarize）→ `gh release` + zip アップロード → アンブレラ submodule pointer
  更新 → `check-org.sh`。

## 落とし穴チートシート

- Civitai `Source` は **version id**（モデルIDではない）。
- Gated HF: `401`＝トークン、`403`＝ライセンス未承諾 → ungated ミラー。
- diffusers 形式の HF repo は単一ファイルpull不可。
- ComfyUI `fp8_scaled` / `fp8_mixed` のエンコーダ/チェックポイントは sd.cpp 非互換。
- SDXL arch 既定の CLIP-skip は 2（アニメ）。実写は 1 に上書き。
- SDXL fp16 は fp16-fix VAE 必須（無いと黒画像）。
- Pony 系は `score_*` プロンプト前置きが必要。
- 黒画像 → VAE/精度、純ノイズ → `Prediction`（eps か v）の誤り。
