package database

import (
	"GoGinMoneyCopilot/models"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"github.com/lib/pq"
)

var DB *sql.DB

var ErrAccountNotFound = errors.New("Account Not Found!")
var ErrUsernameTaken = errors.New("Username Aldready Exist!")
var ErrUserNotFound = errors.New("User Not Found!")
var ErrCategoryNotFound = errors.New("Category Not Found!")
var ErrTransactionNotFound = errors.New("Transaction Not Found!")


func InitDB() { //InitDB should return err or something so you can handle it on !
	connStr := "host=127.0.0.1 port=5432 user=postgres password=1234 dbname=copilot_money sslmode=disable"
	var err error
	DB, err = sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal("Connection is unsuccessful:", err)
	}

	err = DB.Ping()
	if err != nil {
		log.Fatal("Couldn't connect to Database:", err) // Burada direkt killemek yerine değeri döndürüp main'e orada checkle kapamak nasıl olurdu Mentor!
	}
	fmt.Println("Has Been Connected to Database!")

	if err := migrate(); err != nil {
		log.Fatal("Migration failed:", err)
	}
}

func migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS categories (
		id SERIAL PRIMARY KEY,
		name VARCHAR(30) NOT NULL,
		type VARCHAR(10) NOT NULL CHECK (type IN ('income', 'expense')),
		user_id INT REFERENCES users(id)
	);

	CREATE TABLE IF NOT EXISTS transactions (
		id SERIAL PRIMARY KEY,
		account_id INT NOT NULL REFERENCES accounts(id),
		category_id INT NOT NULL REFERENCES categories(id),
		amount NUMERIC(12,2) NOT NULL,
		type VARCHAR(10) NOT NULL CHECK (type IN ('income', 'expense')),
		description VARCHAR(100),
		transaction_date DATE NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT now()
	);
	`
	_, err := DB.Exec(schema)
	return err
}

func CreateAccount(name string, userID int) error {
	query := `INSERT INTO accounts (name, user_id) VALUES ($1, $2);`
	_, err := DB.Exec(query, name, userID)
	if err != nil {
		return fmt.Errorf("Account couldn't be created: %v", err)
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
		return nil, fmt.Errorf("Account Couldn't Be Fetched: %v", err)
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

func CreateUser(username, passwordHash string) error {
	query := `INSERT INTO users (username, password_hash) VALUES ($1, $2);`
	_, err := DB.Exec(query, username, passwordHash)
	if err != nil {
		if pqErr, ok := err.(*pq.Error); ok && pqErr.Code == "23505" {
			return ErrUsernameTaken
		}
		return fmt.Errorf("User couldn't be created: %v", err)
	}
	return nil
}

func GetUserByUsername(username string) (*models.User, error) {
	query := `SELECT id, username, password_hash, is_admin, created_at FROM users WHERE username = $1;`
	row := DB.QueryRow(query, username)

	var user models.User
	err := row.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.IsAdmin, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("User Can't Be Fetched: %v", err)
	}
	return &user, nil
}

func CreateCategory(name, categoryType string, userID *int) error {
	query := `INSERT INTO categories (name, type, user_id) VALUES ($1, $2, $3);`
	_, err := DB.Exec(query, name, categoryType, userID)
	if err != nil {
		return fmt.Errorf("Category couldn't be created: %v", err)
	}
	return nil
}

func GetCategoriesForUser(userID int) ([]models.Category, error) {
	query := `SELECT id, name, type, user_id FROM categories WHERE user_id IS NULL OR user_id = $1;`
	rows, err := DB.Query(query, userID)
	if err != nil {
		return nil, fmt.Errorf("Categories couldn't be fetched: %v", err)
	}
	defer rows.Close()

	var categories []models.Category
	for rows.Next() {
		var cat models.Category
		if err := rows.Scan(&cat.ID, &cat.Name, &cat.Type, &cat.UserID); err != nil {
			return nil, fmt.Errorf("Category row couldn't be read: %v", err)
		}
		categories = append(categories, cat)
	}
	if err := rows.Err(); err != nil {
    	return nil, fmt.Errorf("Rows couldn't be iterated: %v", err)
	}
	
	return categories, nil
}

func CreateTransaction(input models.CreateTransactionInput) error {
	query := `INSERT INTO transactions (account_id, category_id, amount, type, description, transaction_date) VALUES ($1, $2, $3, $4, $5, $6);`
	_, err := DB.Exec(query, input.AccountID, input.CategoryID, input.Amount, input.Type, input.Description, input.TransactionDate)
	if err != nil {
		return fmt.Errorf("Transaction couldn't be created: %v", err)
	}
	return nil
}

func GetTransaction(transactionID int) (*models.Transaction, error) {
	query := `SELECT id, account_id, category_id, amount, type, description, transaction_date, created_at FROM transactions WHERE id = $1;`
	row := DB.QueryRow(query, transactionID)

	var tx models.Transaction
	err := row.Scan(&tx.ID, &tx.AccountID, &tx.CategoryID, &tx.Amount, &tx.Type, &tx.Description, &tx.TransactionDate, &tx.CreatedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrTransactionNotFound
		}
		return nil, fmt.Errorf("Transaction Couldn't Be Fetched: %v", err)
	}
	return &tx, nil
}

func ListTransactionsByAccount(accountID int) ([]models.Transaction, error) {
	query := `SELECT id, account_id, category_id, amount, type, description, transaction_date, created_at FROM transactions WHERE account_id = $1 ORDER BY transaction_date DESC;`
	rows, err := DB.Query(query, accountID)
	if err != nil {
		return nil, fmt.Errorf("Transactions couldn't be fetched: %v", err)
	}
	defer rows.Close()

	var transactions []models.Transaction
	for rows.Next() {
		var tx models.Transaction
		if err := rows.Scan(&tx.ID, &tx.AccountID, &tx.CategoryID, &tx.Amount, &tx.Type, &tx.Description, &tx.TransactionDate, &tx.CreatedAt); err != nil {
			return nil, fmt.Errorf("Transaction row couldn't be read: %v", err)
		}
		transactions = append(transactions, tx)
	}
	if err := rows.Err(); err != nil {
    	return nil, fmt.Errorf("Rows couldn't be iterated: %v", err)
	}

	return transactions, nil
}

func UpdateTransaction(transactionID int, input models.UpdateTransactionInput) error {
	query := `UPDATE transactions SET category_id = $1, amount = $2, type = $3, description = $4, transaction_date = $5 WHERE id = $6;`
	result, err := DB.Exec(query, input.CategoryID, input.Amount, input.Type, input.Description, input.TransactionDate, transactionID)
	if err != nil {
		return fmt.Errorf("Transaction couldn't be updated: %v", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Affected row number didn't come: %v", err)
	}
	if rowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}

func DeleteTransaction(transactionID int) error {
	query := `DELETE FROM transactions WHERE id = $1;`
	result, err := DB.Exec(query, transactionID)
	if err != nil {
		return fmt.Errorf("Transaction can't deleted: %v", err)
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("Affected row number didn't come: %v", err)
	}
	if rowsAffected == 0 {
		return ErrTransactionNotFound
	}
	return nil
}
