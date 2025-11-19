#!/usr/bin/env python3
"""
システムトレイアイコン生成スクリプト
各状態に対応する色付きの●アイコンをICO形式として生成します。
Windows systrayはICO形式を要求します。
"""

from PIL import Image, ImageDraw
import io

def create_circle_icon(color, size=32):
    """指定された色の円形アイコンを生成"""
    # 透明な背景で画像を作成
    img = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)
    
    # アンチエイリアスのために少し大きめに描画
    margin = 4
    draw.ellipse([margin, margin, size-margin, size-margin], fill=color)
    
    return img

def image_to_go_bytes(img):
    """PIL ImageをGoのバイト配列形式に変換（ICO形式）"""
    # ICOとしてバイト列に変換（複数サイズを含む）
    buf = io.BytesIO()
    # 16x16と32x32の両方を含むICOファイルを生成
    img_16 = img.resize((16, 16), Image.Resampling.LANCZOS)
    img.save(buf, format='ICO', sizes=[(16, 16), (32, 32)])
    data = buf.getvalue()
    
    # Goのバイト配列形式に整形
    hex_values = [f'0x{b:02x}' for b in data]
    
    # 12個ずつ改行
    lines = []
    for i in range(0, len(hex_values), 12):
        line = ', '.join(hex_values[i:i+12]) + ','
        lines.append('\t' + line)
    
    return '\n'.join(lines)

# 各状態の色定義
icons = {
    'Idle': (128, 128, 128, 255),      # グレー
    'Watching': (0, 120, 215, 255),    # 青
    'Preparing': (255, 185, 0, 255),   # 黄
    'Running': (16, 124, 16, 255),     # 緑
    'Paused': (255, 140, 0, 255),      # オレンジ
    'Error': (232, 17, 35, 255),       # 赤
}

print("// Package icon は、システムトレイで使用するアイコンデータを保持します。")
print("// 各状態に対応する色付きの●アイコンをICO形式として定義しています。")
print("// このファイルは scripts/generate_icons.py で自動生成されました。")
print("package icon")
print()
print('import "fmt"')
print()

for name, color in icons.items():
    img = create_circle_icon(color)
    go_bytes = image_to_go_bytes(img)
    
    print(f"// Data{name} は、{name}状態のアイコンデータです。")
    print(f"var Data{name} = []byte{{")
    print(go_bytes)
    print("}")
    print()
