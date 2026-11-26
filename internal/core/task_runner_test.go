package core

import "testing"

// TestMatchesSearchKeyword は、search_keywords（複数・OR検索）と
// search_keyword（単一・後方互換）の優先順位およびマッチング判定を検証します。
func TestMatchesSearchKeyword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		title      string
		single     string
		orKeywords []string
		want       bool
	}{
		{
			name:       "OR検索で複数キーワードのうち1つが一致",
			title:      "【朗報】猫が可愛すぎる件",
			single:     "",
			orKeywords: []string{"AI", "猫", "犬"},
			want:       true,
		},
		{
			name:       "OR検索でどのキーワードにも一致しない",
			title:      "今日の飯テロスレ",
			single:     "",
			orKeywords: []string{"AI", "猫", "犬"},
			want:       false,
		},
		{
			name:       "search_keywordsが空の場合はsearch_keywordにフォールバック（一致）",
			title:      "AI生成画像スレ",
			single:     "AI",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "search_keywordsが空の場合はsearch_keywordにフォールバック（不一致）",
			title:      "猫スレ",
			single:     "AI",
			orKeywords: nil,
			want:       false,
		},
		{
			name:       "search_keywordsが空でなければsearch_keywordより優先される",
			title:      "猫スレ",
			single:     "AI", // 単一指定はAIだが、OR側が優先されるため無視される
			orKeywords: []string{"猫"},
			want:       true,
		},
		{
			name:       "両方空なら全スレッドに一致する",
			title:      "何でもいいスレ",
			single:     "",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "search_keywordsが空スライス（nilでない）の場合もsearch_keywordにフォールバック",
			title:      "AI生成画像スレ",
			single:     "AI",
			orKeywords: []string{},
			want:       true,
		},
		// --- 大文字小文字非区別 (REQ-1.2) ---
		{
			name:       "単一キーワードは大文字小文字を区別しない（小文字指定×混在タイトル）",
			title:      "LoRA学習スレ",
			single:     "lora",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "単一キーワードは大文字小文字を区別しない（大文字指定×小文字タイトル）",
			title:      "loraの比較",
			single:     "LORA",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "単一キーワードは大文字小文字を区別しない（LoRA表記）",
			title:      "LoRA注意報",
			single:     "LoRa",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "OR検索は大文字小文字を区別しない",
			title:      "LORA比較スレ",
			single:     "",
			orKeywords: []string{"lora", "版権"},
			want:       true,
		},
		{
			name:       "OR検索は大文字小文字を区別しない（不一致も維持）",
			title:      "LORA比較スレ",
			single:     "",
			orKeywords: []string{"loli", "版権"},
			want:       false,
		},
		{
			name:       "日本語キーワードは従来通り一致する（単一）",
			title:      "猫が可愛いスレ",
			single:     "猫",
			orKeywords: nil,
			want:       true,
		},
		{
			name:       "日本語キーワードは従来通り一致する（OR）",
			title:      "版権スレッド",
			single:     "",
			orKeywords: []string{"猫", "版権"},
			want:       true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := matchesSearchKeyword(tt.title, tt.single, tt.orKeywords)
			if got != tt.want {
				t.Errorf("matchesSearchKeyword(%q, %q, %v) = %v, want %v",
					tt.title, tt.single, tt.orKeywords, got, tt.want)
			}
		})
	}
}

// TestContainsAny は、除外キーワード（exclude_keywords）とOR検索の両方が通る
// containsAny が、大文字小文字を区別せず判定することを検証します（REQ-1.2）。
func TestContainsAny(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		s          string
		substrings []string
		want       bool
	}{
		{
			name:       "除外キーワード: 大文字小文字を区別せず一致する",
			s:          "LoRA注意報",
			substrings: []string{"版権", "lora"},
			want:       true,
		},
		{
			name:       "除外キーワード: 全大文字の指定でも一致する",
			s:          "loraの話",
			substrings: []string{"LORA"},
			want:       true,
		},
		{
			name:       "除外キーワード: 一致しない場合はfalse",
			s:          "LoRA注意報",
			substrings: []string{"loli", "版権"},
			want:       false,
		},
		{
			name:       "日本語キーワードは従来通り一致する",
			s:          "ロリ版権スレ",
			substrings: []string{"版権"},
			want:       true,
		},
		{
			name:       "空リストはfalse",
			s:          "何でもいいスレ",
			substrings: nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := containsAny(tt.s, tt.substrings)
			if got != tt.want {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.substrings, got, tt.want)
			}
		})
	}
}
