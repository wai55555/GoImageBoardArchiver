---
description: 新しい機能開発を始めるためのセットアップワークフロー
---

# 新機能開発セットアップ (New Feature Setup)

このワークフローは、新しい機能を開発する際に、必要なディレクトリ構造やファイルの雛形を自動的に作成するためのものです。

## 手順

1.  **機能名の確認**
    *   ユーザーに開発する機能の名前（英語、ケバブケース推奨。例: `user-login`, `product-list`）を確認します。
    *   以下、機能名を `{feature_name}` とします。

2.  **ディレクトリ作成**
    *   `src/{feature_name}` ディレクトリを作成します。

3.  **ファイル作成**
    *   `src/{feature_name}/index.html` を作成します（基本的なHTML構造を含む）。
    *   `src/{feature_name}/style.css` を作成します。
    *   `src/{feature_name}/script.js` を作成します。

4.  **確認**
    *   作成されたファイル構成をユーザーに報告します。
