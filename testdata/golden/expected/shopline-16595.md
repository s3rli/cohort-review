# api.shoplineapp.com#16595 — 刪除型 (15,393 行 · 219 檔 · 96.6% 刪除)
3 組:
1. [claim] trigger_qa_api_test.sh 新腳本 (403 行, 唯一逐行 review 對象)
2. [claim] pipeline 換心臟 (113 行 yml) — 藏最重要決策 on-fail: strategy: ignore 軟上線
3. [deletion] 217 檔 vendored Karate suite (14,877 行) — claim 三問

驗收重點 (數字為 2026-09-04 實測, 見 harness 輸出):
- 摺疊生效: 送模型的 hunk 內容 199.5KB → 31.4KB, omitted 檔數 102 → 0
  (handoff 訂的 <30KB 目標仍差約 1.4KB, 尚未達標 — 不是 pass)
- 217 刪除檔歸一個 deletion cohort
