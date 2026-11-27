// Package catalog 是 contracts.Catalog 契約的唯一擁有者模組。
// 它用一般建構子註冊具象實作 *Service，並於 Configure 階段呼叫 Export
// 將 contracts.Catalog 公開給直接相依於自己的消費模組。
package catalog

import (
	"context"
	"fmt"

	"example.com/moduleapp/catalog/contracts"
	"example.com/moduleapp/store"
	"github.com/pangolin-framework/pangolin/kernel/module"
)

// Service 是 catalog 模組內部的具象實作（滿足 contracts.Catalog 介面）。
// 注意：建構子 NewService 參數直接宣告 *store.Store，DI 引擎會自動從直接相依的 store 模組解析注入。
type Service struct {
	store *store.Store
}

// NewService 是註冊給 DI 組合引擎的建構子，回傳具體型別 *Service。
func NewService(s *store.Store) *Service {
	return &Service{store: s}
}

// Put 寫入底層共用 store。
func (s *Service) Put(id, title string) error {
	if err := s.store.Put(id, title); err != nil {
		return err
	}
	return nil
}

// Get 從底層 store 讀取並轉為契約定義的純資料結構 contracts.Item。
func (s *Service) Get(id string) (contracts.Item, bool) {
	title, ok := s.store.Get(id)
	return contracts.Item{ID: id, Title: title}, ok
}

// Len 回報目前 catalog 筆數。
func (s *Service) Len() int { return s.store.Len() }

// Module 是 catalog 的模組定義，同時也是 contracts.Catalog 契約的單一擁有者。
type Module struct{}

func (m *Module) ModuleMetadata() module.Metadata {
	return module.Metadata{Name: "catalog", Version: "0.1.0"}
}

// DependsOn 宣告直接相依於 store.Module，讓 NewService 能合法取得 *store.Store。
func (m *Module) DependsOn() []module.Ref {
	return []module.Ref{module.Of[store.Module]()}
}

func (m *Module) Configure(b module.RootBuilder) error {
	// 步驟 1：以 Singleton 註冊具象實作的建構子。
	if err := module.AddSingleton(b.Services(), NewService); err != nil {
		return err
	}
	// 步驟 2：由契約擁有者明確呼叫 Export，將 contracts.Catalog 公開出去。
	// 每個契約介面全系統只能有一個擁有者 Export，避免多模組暗中互搶實作。
	return b.Services().Export(module.Contract[contracts.Catalog]())
}

func (m *Module) Start(ctx context.Context, rt module.Runtime) error {
	// 透過型別安全的泛型 Resolve 取得公開契約並寫入示範資料。
	service, err := module.Resolve[contracts.Catalog](ctx, rt.Services())
	if err != nil {
		return err
	}
	if err := service.Put("demo", "root-composed"); err != nil {
		return err
	}
	item, ok := service.Get("demo")
	fmt.Printf("start catalog via %T: %v/%v=%q\n", service, item.ID, ok, item.Title)
	return nil
}

func (m *Module) Stop(context.Context, module.Runtime) error {
	fmt.Println("stop catalog")
	return nil
}
