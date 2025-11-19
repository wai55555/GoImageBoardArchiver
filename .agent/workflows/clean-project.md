---
description: プロジェクトの一時ファイルや依存関係（node_modules等）を削除してクリーンにする
---

# プロジェクトクリーンアップ (Project Cleanup)

このワークフローは、`node_modules`、`dist`、`.cache` などの生成されたディレクトリを削除し、プロジェクトをクリーンな状態に戻します。依存関係の再インストール前や、ディスク容量の節約に役立ちます。

## 手順

1.  **確認**
    *   ユーザーにクリーンアップを実行してよいか最終確認します。「`node_modules` などを削除しますか？」

2.  **削除実行 (PowerShell)**
    *   以下のフォルダが存在すれば削除します。
        *   `node_modules`
        *   `dist`
        *   `build`
        *   `.cache`
        *   `.next` (Next.jsの場合)
    *   コマンド例:
        ```powershell
        Remove-Item -Path node_modules, dist, build, .cache, .next -Recurse -Force -ErrorAction SilentlyContinue
        ```

3.  **完了報告**
    *   削除が完了したことを報告します。
