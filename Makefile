.PHONY: run run-log test test-integration docker-up docker-down docker-logs docker-reset

run:
	go run ./src/main.go

run-log:
	rm -f app.log
	go run ./src/main.go > app.log 2>&1

# Hızlı birim testleri (sahte repo, DB gerektirmez).
test:
	go test ./...

# Gerçek Postgres'e karşı entegrasyon testleri (copilot_money_test'i otomatik kurar).
test-integration:
	go test -tags integration ./repositories/

# --- Docker ---
docker-up:      ## sistemi ayağa kaldır (build + arka plan)
	docker compose up --build -d
docker-down:    ## durdur (veritabanı volume KORUNUR)
	docker compose down
docker-logs:    ## uygulama loglarını takip et
	docker compose logs -f app
docker-reset:   ## HER ŞEYİ sil, veritabanı dahil (volume gider)
	docker compose down -v