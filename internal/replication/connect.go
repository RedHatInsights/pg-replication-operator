package replication

import (
	"database/sql"
	"fmt"

	_ "github.com/lib/pq"
)

func DBConnect(credentials DatabaseCredentials) (*sql.DB, error) {
	connStr := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		credentials.Host, credentials.Port, credentials.User, credentials.Password, credentials.DatabaseName, "disable")
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return nil, err
	}
	err = db.Ping()
	return db, err
}

func CheckPublication(db *sql.DB, name string) error {
	row := db.QueryRow(`SELECT p.puballtables,
							   (p.pubinsert AND p.pubupdate AND p.pubdelete AND p.pubtruncate) as pubops,
							   (SELECT COUNT(*) FROM pg_publication_namespace pn WHERE p.oid = pn.pnpubid) as pubnamespaces
						  FROM pg_publication p
						 WHERE p.pubname = $1`, name)
	var (
		puballtables  bool
		pubops        bool
		pubnamespaces int
	)

	err := row.Scan(&puballtables, &pubops, &pubnamespaces)
	if err != nil {
		if err == sql.ErrNoRows {
			err = fmt.Errorf("publication '%s' does not exist", name)
		}
		return err
	}

	// publication should not be FOR ALL TABLES
	// allow insert/update/delete/truncate
	// should not be FOR TABLES IN SCHEMA
	if puballtables ||
		!pubops ||
		pubnamespaces > 0 {
		err := fmt.Errorf("publication '%s' has wrong attributes", name)
		return err
	}

	return nil
}
