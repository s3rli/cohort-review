# callback-service-api-gateway#18 — 平行意圖型 (609 行 · 9 檔 · easy mode)
7 組:
1. [claim] 主幹: 非 JSON body → form fallback — flag 兩個 yaml + resolveBodyFormat +
   transformFormPayload + wrapper/isJSONObject + 配套測試 (~250 行)
2. [claim] JSON root 類型 — object/string 過; array/number/bool/null → ErrBypassSender
3. [claim] 空 body → {}
4. [claim] Family Mart 錯誤 → 200 fallback
5. [claim] content-type 統一 application/json
6. [mechanical] NIT: pulsar log 刪 1 行 + DisableDefaultContentType 機械加進 ~10 個測試 context
7. [misc] payload string→[]byte 型別重構 — description 沒宣告 (應排最前)

關鍵: order_payment_with_custom_response.yaml 一檔跨 ①⑤ 兩組 (hunk-level);
fallbackResponse() 同時服務 ④⑤, 雙屬可接受 (v1 first-claim-wins, 可容忍).
