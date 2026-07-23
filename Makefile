.PHONY: run run-log test test-integration

run:
	go run ./src/main.go

run-log:
	rm -f app.log
	go run ./src/main.go > app.log 2>&1

# Hızlı birim testleri (sahte repo, DB gerektirmez).
test:
	go test ./...

# Gerçek Postgres'e karşı entegrasyon testleri. copilot_money_test veritabanını
# otomatik kurar; GERÇEK veriye (copilot_money) dokunmaz. DB_* env gerekir.
test-integration:
	go test -tags integration ./repositories/
