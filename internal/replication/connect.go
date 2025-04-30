package replication

import (
	"database/sql"
	"fmt"
	"log"

	"github.com/lib/pq"
)

func credentialsToConnectionString(credentials DatabaseCredentials) string {
	return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		credentials.Host, credentials.Port, credentials.User, credentials.Password, credentials.DatabaseName, "disable")
}

func DBConnect(credentials DatabaseCredentials) (*sql.DB, error) {
	connStr := credentialsToConnectionString(credentials)
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
			return fmt.Errorf("publication '%s' does not exist", name)
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

func CreateSubscription(db *sql.DB, name string, connStr string) error {
	sql := fmt.Sprintf(`CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s WITH (connect=false);`,
		pq.QuoteIdentifier(name),
		pq.QuoteLiteral(connStr),
		pq.QuoteIdentifier(name))
	_, err := db.Exec(sql)
	if err != nil {
		return err
	}
	log.Printf("subscription '%s' created", name)
	return nil
}

func AlterSubscription(db *sql.DB, name string, connStr string) error {
	sql := fmt.Sprintf("ALTER SUBSCRIPTION %s CONNECTION %s", pq.QuoteIdentifier(name), pq.QuoteLiteral(connStr))
	_, err := db.Exec(sql)
	if err != nil {
		return err
	}
	sql = fmt.Sprintf("ALTER SUBSCRIPTION %s ENABLE", pq.QuoteIdentifier(name))
	_, err = db.Exec(sql)
	if err != nil {
		return err
	}
	log.Printf("subscription '%s' updated", name)
	return nil
}

func CheckSubscription(db *sql.DB, name string, credentials DatabaseCredentials) error {
	row := db.QueryRow(`SELECT s.subenabled,
							   s.subconninfo
						  FROM pg_subscription s
						 WHERE s.subname = $1`, name)
	var (
		subenabled  bool
		subconninfo string
	)

	connStr := credentialsToConnectionString(credentials)
	err := row.Scan(&subenabled, &subconninfo)
	if err != nil {
		if err == sql.ErrNoRows {
			err = CreateSubscription(db, name, connStr)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	// subscription should be enabled
	// and have correct connection info
	if !subenabled ||
		subconninfo != connStr {
		log.Printf("subscription '%s' has wrong attributes", name)
		err = AlterSubscription(db, name, connStr)
		if err != nil {
			return err
		}
	}

	return nil
}
