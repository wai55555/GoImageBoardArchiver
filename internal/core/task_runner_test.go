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
