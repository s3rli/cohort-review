# api.shoplineapp.com#16595 — 刪除型 (15,393 行 · 219 檔 · 96.6% 刪除)
3 組:
1. [claim] trigger_qa_api_test.sh 新腳本 (403 行, 唯一逐行 review 對象)
2. [claim] pipeline 換心臟 (113 行 yml) — 藏最重要決策 on-fail: strategy: ignore 軟上線
3. [deletion] 217 檔 vendored Karate suite (14,877 行) — claim 三問

驗收重點: 摺疊生效 — 送模型的 hunk 內容 < 30KB (prompt 大小印在 harness 輸出);
217 刪除檔歸一個 deletion cohort; omitted 檔數應為 0 (unfolded 時 ~157).
