.PHONY: run run-log

run:
	go run ./src/main.go

run-log:
	rm -f app.log
	go run ./src/main.go > app.log 2>&1
	