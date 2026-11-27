package main

import (
	"fmt"
	"log"
	"os"

	"github.com/cymondez/pangolin-framework/kernel/config"
)

func main() {
	// 步驟 1：由應用入口明確挑選要接受的環境變數，對應到設定鍵 "app.name"。
	// panGOlin 不會自動掃描整個系統環境，避免無關變數意外污染應用組態。
	environment := map[string]string{}
	if name, ok := os.LookupEnv("APP_NAME"); ok {
		environment["app.name"] = name
	}

	// 步驟 2：建立 Loader 並宣告驗證規則（Required: true 代表不可缺漏或為空白）。
	// Load 會依序套用來源（右邊覆寫左邊：defaults < environment），並在回傳前執行全部規則檢查。
	settings, err := config.NewLoader([]config.Rule{
		{Key: "app.name", Required: true},
	}).Load([]config.Source{
		{Name: "defaults", Values: map[string]string{"app.name": "My App"}},
		{Name: "environment", Values: environment},
	})
	if err != nil {
		// 必要設定缺漏或驗證失敗時，在啟動階段直接中止，不讓應用帶著非法設定上線。
		log.Fatal(err)
	}

	// 步驟 3：Load 通過後得到只讀的 Reader，可安全在應用各處查詢最終生效值。
	name, _ := settings.Lookup("app.name")
	fmt.Println("Application:", name)
}
