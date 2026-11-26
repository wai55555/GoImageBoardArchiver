# internal/adapter/testdata

ふたばアダプタのテスト用フィクスチャ。

## 方針 (Phase 4 / REQ-4.1)

- **実サイトの HTML は置かない。** すべてふたばのマークアップ構造を再現した
  **人工データ**で、実在の投稿・画像・個人情報を含まない。
- 文字コードはふたばの実配信と同じ **Shift_JIS**。`ParseCatalog` / `ParseThreadHTML`
  の文字コード変換経路もテストできるようにするため。
- `.gitignore` の除外対象から外し、コミットする (fresh clone でテストが通るようにする)。

## ファイル

| ファイル | 用途 |
|---|---|
| `futaba_catalog_long_title.html` | カタログ解析: 長いタイトル、相対パス |
| `futaba_thread_normal.html` | スレッド解析/再構成: 通常のメディアリンク |
| `futaba_thread_edge_cases.html` | スレッド解析: 二重引用符/単一引用符/重複/桁数不足 |
| `futaba_catalog_equivalence.html` | goquery 移行の等価性: `<br>`/タグ/エンティティ、空タイトル、重複 ID |
| `futaba_thread_equivalence.html` | goquery 移行の等価性: 引用符なし href、jpg/png/mp4/webm |

## 更新方法

UTF-8 で編集した内容を Shift_JIS で保存する。

```sh
iconv -f UTF-8 -t SHIFT_JIS input.html > internal/adapter/testdata/<name>.html
```
