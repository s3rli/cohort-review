# backburner#7 — 主從結構型 (918 行 · 16 檔 · description 空白, hard mode)
5 組 + misc, 讀序 = 機制 → 接線 → 善後:
1. [claim] 核心模組 OtelJobTracing + 專屬測試 + CI otel-test step (~59%)
2. [claim] producer 接線 — worker.rb enqueue + worker_test
3. [claim] consumer 接線 — job.rb process + job_test
4. [claim] 子行程退出前 flush — configuration + flush_open_telemetry +
   forking/threads_on_fork + 兩個 worker test
5. [nonfix] facade 契約 — backburner.rb 文件 + README facade 段 + back_burner_test 釘現狀
   (pre-existing 問題刻意不修, README + 測試當緩解)
misc: string→[]byte 型別重構 (transformer.go + basic_transformer.go) — 排最前

關鍵: description 0 字, CHANGELOG 12 行是作者的意圖摘要 (W5 in-diff 錨點實例).
