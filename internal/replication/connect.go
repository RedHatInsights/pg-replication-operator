package replication

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"

	"github.com/lib/pq"
)

func credentialsToConnectionString(credentials DatabaseCredentials) string {
	return fmt.Sprintf("postgresql://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(credentials.User),
		url.QueryEscape(credentials.Password),
		url.QueryEscape(credentials.Host),
		url.QueryEscape(credentials.Port),
		url.QueryEscape(credentials.DatabaseName),
		url.QueryEscape("disable"))
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
	rows, err := db.Query(`SELECT p.puballtables,
								  (p.pubinsert AND p.pubupdate AND p.pubdelete AND p.pubtruncate) as pubops,
								  (SELECT COUNT(*) FROM pg_publication_namespace pn WHERE p.oid = pn.pnpubid) as pubnamespaces
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
		log.Print(err)
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
		log.Print(err)
		return err
	}

	return nil
}

func CheckSubscription(db *sql.DB, name string, credentials DatabaseCredentials) error {
	rows, err := db.Query(`SELECT s.subenabled,
								  s.subconninfo
							 FROM pg_subscription s
							WHERE s.subname = $1`, name)
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	var (
		subenabled  bool
		subconninfo string
	)

	connStr := credentialsToConnectionString(credentials)
	if !rows.Next() {
		log.Printf("subscription '%s' does not exist", name)
		sql := fmt.Sprintf(`CREATE SUBSCRIPTION %s CONNECTION %s PUBLICATION %s WITH (connect=false)`,
			pq.QuoteIdentifier(name),
			pq.QuoteLiteral(connStr),
			pq.QuoteIdentifier(name))
		_, err = db.Exec(sql)
		if err != nil {
			log.Print(err)
			return err
		}
		log.Printf("subscription '%s' created", name)
		return nil
	}

	err = rows.Scan(&subenabled, &subconninfo)
	if err != nil {
		log.Print(err)
		return err
	}

	// subscription should be enabled
	// and have correct connection info
	if !subenabled ||
		subconninfo != connStr {
		log.Printf("subscription '%s' has wrong attributes", name)
		_, err = db.Exec("ALTER SUBSCRIPTION " + pq.QuoteIdentifier(name) + " CONNECTION " + pq.QuoteLiteral(connStr))
		if err != nil {
			log.Print(err)
			return err
		}
		_, err = db.Exec("ALTER SUBSCRIPTION " + pq.QuoteIdentifier(name) + " ENABLE")
		if err != nil {
			log.Print(err)
			return err
		}
		log.Printf("subscription '%s' updated", name)
	}

	return nil
}
