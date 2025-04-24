package replication

import (
	"database/sql"
	"fmt"
	"log"
	"reflect"
	"strings"

	"github.com/lib/pq"
)

type PgTableColumn struct {
	Name                   string
	Default                sql.NullString
	Nullable               bool
	Type                   string
	CharacterMaximumLength sql.NullInt32
	NumericPrecision       sql.NullInt32
	NumericScale           sql.NullInt32
	DatetimePrecision      sql.NullInt32
}

type PgTable struct {
	Schema  string
	Name    string
	Columns []PgTableColumn
}

type PgIndex struct {
	Name string
	Def  string
}

func PublicationTables(db *sql.DB, pubname string) ([]PgTable, error) {
	rows, err := db.Query(`SELECT n.nspname AS schema, r.relname AS name
							 FROM pg_publication p
							 JOIN pg_publication_rel pr ON p.oid = pr.prpubid
							 JOIN pg_class r ON pr.prrelid = r.oid
							 JOIN pg_namespace n ON r.relnamespace = n.oid
							 WHERE p.pubname = $1`, pubname)
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	var (
		schema string
		name   string
	)
	tables := make([]PgTable, 0, 5)

	for rows.Next() {
		err = rows.Scan(&schema, &name)
		if err != nil {
			log.Print(err)
			return nil, err
		}
		tables = append(tables, PgTable{Schema: schema, Name: name})
	}
	return tables, nil
}

func PublicationTableDetail(db *sql.DB, table *PgTable) error {
	rows, err := db.Query(`SELECT column_name,
								  column_default,
								  (is_nullable = 'YES'),
								  data_type,
								  character_maximum_length,
								  numeric_precision,
								  numeric_scale,
								  datetime_precision
							 FROM information_schema.columns c
							 JOIN pg_publication_tables pt
							   ON c.table_schema = pt.schemaname
							  AND c.table_name = pt.tablename
							  AND c.column_name = ANY(pt.attnames)
							WHERE table_schema = $1 AND table_name = $2
							ORDER BY c.ordinal_position`,
		table.Schema, table.Name)
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	columns := make([]PgTableColumn, 0)
	for rows.Next() {
		var col PgTableColumn
		err := rows.Scan(
			&col.Name,
			&col.Default,
			&col.Nullable,
			&col.Type,
			&col.CharacterMaximumLength,
			&col.NumericPrecision,
			&col.NumericScale,
			&col.DatetimePrecision,
		)
		if err != nil {
			return err
		}
		columns = append(columns, col)
	}

	table.Columns = columns
	return nil
}

func CheckSubscriptionSchema(db *sql.DB, name string) error {
	rows, err := db.Query(`SELECT true
							 FROM pg_namespace n
							 WHERE n.nspname = $1`, name)
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	if !rows.Next() {
		log.Printf("schema '%s' does not exist", name)
		sql := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pq.QuoteIdentifier(name))
		_, err = db.Exec(sql)
		if err != nil {
			log.Print(err)
			return err
		}
		log.Printf("schema '%s' created", name)
	}
	return nil
}

func equalColumns(a, b []PgTableColumn) bool {
	if len(a) != len(b) {
		return false
	}
	// works only with exported fields (uppercase names) and not pointers
	for i := range a {
		if !reflect.DeepEqual(a[i], b[i]) {
			return false
		}
	}
	return true
}

func createColumns(columns []PgTableColumn) string {
	columnDefs := make([]string, len(columns))
	for i, col := range columns {
		columnDefs[i] = col.Name + " " + col.Type
		if col.CharacterMaximumLength.Valid {
			columnDefs[i] += fmt.Sprintf(" (%d)", col.CharacterMaximumLength.Int32)
		}
		if col.NumericPrecision.Valid {
			columnDefs[i] += fmt.Sprintf(" (%d, %d)", col.NumericPrecision.Int32, col.NumericScale.Int32)
		}
		if col.DatetimePrecision.Valid {
			columnDefs[i] += fmt.Sprintf(" (%d)", col.DatetimePrecision.Int32)
		}
		if !col.Nullable {
			columnDefs[i] += " NOT NULL"
		}
		if col.Default.Valid {
			columnDefs[i] += " DEFAULT " + col.Default.String
		}
	}
	return strings.Join(columnDefs, ", ")
}

func CheckSubscriptionTableDetail(db *sql.DB, publicationName string, table PgTable) error {
	rows, err := db.Query(`SELECT column_name,
								  column_default,
								  (is_nullable = 'YES'),
								  data_type,
								  character_maximum_length,
								  numeric_precision,
								  numeric_scale,
								  datetime_precision
							 FROM information_schema.columns c
							WHERE table_schema = $1 AND table_name = $2
							ORDER BY c.ordinal_position`,
		table.Schema, table.Name)
	if err != nil {
		log.Print(err)
		return err
	}
	defer rows.Close()

	columns := make([]PgTableColumn, 0)
	for rows.Next() {
		var col PgTableColumn
		err := rows.Scan(
			&col.Name,
			&col.Default,
			&col.Nullable,
			&col.Type,
			&col.CharacterMaximumLength,
			&col.NumericPrecision,
			&col.NumericScale,
			&col.DatetimePrecision,
		)
		if err != nil {
			return err
		}
		columns = append(columns, col)
	}

	if !equalColumns(table.Columns, columns) {
		log.Printf("table %s.%s differs from publication", table.Schema, table.Name)

		sql := fmt.Sprintf(`DROP TABLE IF EXISTS %s.%s`, pq.QuoteIdentifier(table.Schema), pq.QuoteIdentifier(table.Name))
		_, err = db.Exec(sql)
		if err != nil {
			log.Print(err)
			return err
		}

		tableColumns := createColumns(table.Columns)
		sql = fmt.Sprintf(`CREATE TABLE %s.%s (%s)`,
			pq.QuoteIdentifier(table.Schema), pq.QuoteIdentifier(table.Name), tableColumns)
		_, err = db.Exec(sql)
		if err != nil {
			log.Print(err)
			return err
		}

		/*
			sql = fmt.Sprintf(`DROP VIEW IF EXISTS "%s"."%s";`, table.Schema, table.Name)
			_, err = db.Exec(sql)
			if err != nil {
				log.Print(err)
				return err
			}

			sql = fmt.Sprintf(`CREATE VIEW %s.%s AS SELECT * FROM %s.%s`,
				table.Schema, table.Name, table.Schema, tablePublicationName)
			_, err = db.Exec(sql)
			if err != nil {
				log.Print(err)
				return err
			}
		*/

		// create indexes
		rows, err = db.Query(`SELECT indexname,
									 indexdef
								FROM pg_indexes
							   WHERE schemaname = $1 AND tablename = $2`,
			table.Schema, table.Name)
		if err != nil {
			log.Print(err)
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var idx PgIndex
			err := rows.Scan(
				&idx.Name,
				&idx.Def,
			)
			if err != nil {
				return err
			}
			_, err = db.Exec(idx.Def)
			if err != nil {
				log.Print(err)
				return err
			}
		}
	}
	return nil
}

func CheckSubscriptionView(db *sql.DB, schema string, table PgTable) error {
	rows, err := db.Query(`SELECT true
							 FROM pg_views v
							 WHERE v.schemaname = $1 AND v.viewname = $2`, schema, table.Name)
	if err != nil {
		log.Print(err)
	}
	defer rows.Close()

	if !rows.Next() {
		sql := fmt.Sprintf(`CREATE VIEW %s.%s AS SELECT * FROM %s.%s`,
			pq.QuoteIdentifier(schema), pq.QuoteIdentifier(table.Name),
			pq.QuoteIdentifier(table.Schema), pq.QuoteIdentifier(table.Name))
		_, err = db.Exec(sql)
		if err != nil {
			log.Print(err)
			return err
		}
	}
	return nil
}
