# moduleapp/pricing：預設 Transient 與私有服務

`Module` 以預設 Transient 註冊 `NewPrices`（未給 life 選項），示範預設生命週期：每次解析都由建構子重建。`*Prices` 依賴共用 store；本模組沒有 Export，因此 `*Prices` 對其他模組（含 app 根）不可見——Start 以自身受限 Resolver 解析自己的具象服務，示範「無契約也可解析自己」。Stop 無自有資源。
