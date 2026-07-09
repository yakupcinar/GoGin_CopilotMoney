package database

import (
	"GoGinMoneyCopilot/models"
	"database/sql"
	"errors"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

var DB *sql.DB

var ErrAccountNotFound = errors.New("hesap bulunamadı")

func InitDB() {
	connStr := "host=127.0.0.1 port=5432 user=postgres password=1234 dbname=copilot_money sslmode=disable"
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Bağlantı ayarı başarısız:", err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatal("Veritabanına bağlanılamadı:", err)
	}
	fmt.Println("Veritabanına başarıyla bağlanıldı!")
}

func CreateAccount(name string, userID int) error {
	query := `INSERT INTO accounts (name, user_id) VALUES ($1, $2);`
	_, err := DB.Exec(query, name, userID)
	if err != nil {
		return fmt.Errorf("hesap oluşturulamadı: %v", err)
	}
	return nil
}

func GetAccount(accountID int) (*models.Account, error) {
	query := `SELECT id, name, user_id, created_at FROM accounts WHERE id = $1;`
	row := DB.QueryRow(query, accountID)

	var acc models.Account
	err := row.Scan(&acc.ID, &acc.Name, &acc.UserID, &acc.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAccountNotFound
		}
		return nil, fmt.Errorf("hesap getirilemedi: %v", err)
	}

	return &acc, nil
}

func UpdateAccount(accountID int, name string) error {
	query := `UPDATE accounts SET name = $1 WHERE id = $2;`
	result, err := DB.Exec(query, name, accountID)
	if err != nil {
		return fmt.Errorf("Account couldn't be deleted: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Affected row number didn't come: %v", err)
	}

	if rowsAffected == 0 {
		return ErrAccountNotFound
	}
	return nil
}

func DeleteAccount(accountID int,) error {
	query := `DELETE FROM accounts WHERE id = $1;`
	result, err := DB.Exec(query, accountID)
	if err != nil {
		return fmt.Errorf("Account can't deleted: %v", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Affected row number didn't come: %v", err)
	}

	if rowsAffected == 0 {
    	return ErrAccountNotFound
	}
	return nil
}
