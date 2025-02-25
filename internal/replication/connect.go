package replication

import (
	"database/sql"
	"fmt"
	"log"

	_ "github.com/lib/pq"
)

func DBConnect(credentials DatabaseCredentials) (*sql.DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		credentials.Host, credentials.Port, credentials.User, credentials.Password, credentials.DatabaseName, "disable")
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	err = db.Ping()
	if err != nil {
		log.Fatal(err)
	}
	return db, nil
}
