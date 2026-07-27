package database

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/redis/go-redis/v9"
)

// Redis bağlantısı — access token denylist'inin TAM KOPYASI için.
//
// NEDEN CACHE DEĞİL KOPYA: denylist'te "Redis'te yok" ifadesi bir güvenlik
// kararı üretir. Kısmi bir cache'te bu ifade belirsizdir ("iptal edilmemiş"
// mi, "henüz cache'lenmemiş" mi?) ve iki yorumu da hatalıdır. Küme küçük
// olduğu için (yalnızca son ACCESS_TOKEN_TTL içindeki logout'lar) tamamını
// Redis'te tutuyoruz; böylece yokluk KESİN olarak "iptal edilmemiş" demek.
//
// Kalıcı kaynak Postgres'tir. Redis yeniden başlarsa WarmUp onu doldurur.

// RDB nil ise Redis devre dışıdır ve denylist doğrudan Postgres'ten okunur.
// GROQ_API_KEY deseninin aynısı: yapılandırma yoksa özellik kapalı, uygulama ayakta.
var RDB *redis.Client

// InitRedis — REDIS_ADDR boşsa sessizce devre dışı bırakır (hata değil).
func InitRedis() error {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		return nil
	}

	client := redis.NewClient(&redis.Options{Addr: addr})

	// NewClient HİÇBİR ŞEY bağlamaz; sadece istemci nesnesi kurar ve gerçek
	// bağlantı ilk komutta açılır. Ping atmazsak yanlış adres/kapalı Redis
	// başlangıçta fark edilmez, ilk IsRevoked çağrısında patlar.
	// InitDB ile aynı refleks: hatayı trafik almadan önce gör.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("redis is unreachable (%s): %w", addr, err)
	}

	RDB = client
	fmt.Println("Has Been Connected to Redis!")
	return nil
}

// CloseRedis — graceful shutdown sırasında çağrılır.
func CloseRedis() error {
	if RDB == nil {
		return nil
	}
	return RDB.Close()
}
