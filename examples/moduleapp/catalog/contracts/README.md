# moduleapp/catalog/contracts：公開介面契約

`Catalog` 介面與 `Item` 值是 catalog 模組的公開契約。契約擁有者是 catalog 模組（以 Export 建立），不是本 package 的任何實作；其他模組只可在建構子層級實作此介面，跨模組可見性一律經 catalog 的 Export 或安裝方的明示 `As` 貢獻，結構滿足不產生關聯。本 package 只含介面與資料值，供消費方 import。
