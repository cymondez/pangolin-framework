// Package contracts 發布 catalog 模組的公開介面契約。
// 跨模組互動原則：只把「需要對外公開的介面與純資料 DTO」放在獨立 contracts 套件；
// 由契約擁有者（catalog 模組）單一負責 Export，消費模組透過直接相依解析此契約。
// 其他模組即使結構上實作了相同方法，若未經明確關聯宣告也不會自動外洩成公開實作。
package contracts

// Catalog 是 catalog 模組對外承諾的公開讀寫介面契約。
type Catalog interface {
	Put(id, title string) error
	Get(id string) (Item, bool)
	Len() int
}

// Item 是跨模組傳遞的純值物件（DTO），不包含任何內部實作細節或資料庫型別。
type Item struct {
	ID    string
	Title string
}
