# moduleapp/store：菱形共用相依

`Module` 被 catalog 與 pricing 同時以 Type 參照，loader 依 Type 去重後只規劃、建構一次。`Add(NewStore, Singleton)` 明示 Singleton；`*Store` 經本模組 Export 後成為可跨模組解析的具象契約。`Start` 透過受限 Resolver 解析自身 Export 的 `*Store` 並輸出診斷；`Stop` 輸出停機診斷。
