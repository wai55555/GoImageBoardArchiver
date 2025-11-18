package adapter

import (
	"fmt"
)

// adapterRegistry は、サイト名とSiteAdapter実装のマッピングを保持します。
var adapterRegistry = map[string]func() SiteAdapter{
	"futaba": NewFutabaAdapter,
}

// GetAdapter は、指定されたサイト名に対応するSiteAdapterの新しいインスタンスを返します。
// ファクトリパターンを使用することで、新しいサイトアダプタの追加を容易にします。
func GetAdapter(siteName string) (SiteAdapter, error) {
	factory, ok := adapterRegistry[siteName]
	if !ok {
		return nil, fmt.Errorf("サイト名 '%s' に対応するアダプタが見つかりません", siteName)
	}
	return factory(), nil
}
