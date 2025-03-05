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

func CheckPublication(db *sql.DB, name string) error {
	rows, err := db.Query(`SELECT p.puballtables,
								  (p.pubinsert and p.pubupdate and p.pubdelete and p.pubtruncate) as pubops,
								  (select count(*) from pg_publication_namespace pn where p.oid = pn.pnpubid) as pubnamespaces
							 FROM pg_publication p
							WHERE p.pubname = $1`, name)
	if err != nil {
		log.Print(err)
		return err
	}
	defer rows.Close()

	var (
		puballtables  bool
		pubops        bool
		pubnamespaces int
	)

	if !rows.Next() {
		err := fmt.Errorf("publication '%s' does not exist", name)
		log.Fatal(err)
		return err
	}

	err = rows.Scan(&puballtables, &pubops, &pubnamespaces)
	if err != nil {
		log.Print(err)
		return err
	}

	// publication should not be FOR ALL TABLES
	// allow insert/update/delete/truncate
	// should not be FOR TABLES IN SCHEMA
	if puballtables ||
		!pubops ||
		pubnamespaces > 0 {
		err := fmt.Errorf("publication '%s' has wrong attributes", name)
		log.Fatal(err)
		return err
	}

	return nil
}
