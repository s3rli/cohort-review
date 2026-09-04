golden:
	go test -tags golden -run TestGolden -v -count=1 -timeout 30m .

.PHONY: golden
