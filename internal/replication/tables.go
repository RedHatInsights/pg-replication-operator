package replication

import (
	"database/sql"
	"fmt"
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
	Schema string `json:"schema"`
	Name   string `json:"name"`
}

type PgTableDetail struct {
	PgTable
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
		return []PgTable{}, err
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
			return nil, err
		}
		tables = append(tables, PgTable{Schema: schema, Name: name})
	}
	return tables, nil
}

func tableColumns(db *sql.DB, table PgTable, joinPublication bool) (PgTableDetail, error) {
	sqlJoin := ""
	if joinPublication {
		sqlJoin = `JOIN pg_publication_tables pt
                     ON c.table_schema = pt.schemaname
                    AND c.table_name = pt.tablename
                    AND c.column_name = ANY(pt.attnames)`
	}
	sql := fmt.Sprintf(`SELECT column_name,
							   column_default,
							  (is_nullable = 'YES'),
							  data_type,
							  character_maximum_length,
							  numeric_precision,
							  numeric_scale,
							  datetime_precision
						 FROM information_schema.columns c
						 %s
						WHERE c.table_schema = $1 AND c.table_name = $2
						ORDER BY c.ordinal_position`,
		sqlJoin)
	tableDetail := PgTableDetail{}
	rows, err := db.Query(sql, table.Schema, table.Name)
	if err != nil {
		return tableDetail, err
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
			return tableDetail, err
		}
		columns = append(columns, col)
	}

	tableDetail.Schema = table.Schema
	tableDetail.Name = table.Name
	tableDetail.Columns = columns
	return tableDetail, nil
}

func PublicationTableDetail(db *sql.DB, table PgTable) (PgTableDetail, error) {
	return tableColumns(db, table, true)
}

func CreateSubscriptionSchema(db *sql.DB, name string) error {
	sql := fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pq.QuoteIdentifier(name))
	_, err := db.Exec(sql)
	return err
}

func CheckSubscriptionSchema(db *sql.DB, name string) error {
	row := db.QueryRow(`SELECT true
						  FROM pg_namespace n
						 WHERE n.nspname = $1`, name)
	var exists bool
	err := row.Scan(&exists)
	return err
}

func CheckSubscriptionTable(db *sql.DB, table PgTable) error {
	row := db.QueryRow(`SELECT true
						  FROM pg_tables
						 WHERE schemaname = $1 AND tablename = $2`, table.Schema, table.Name)
	var exists bool
	err := row.Scan(&exists)
	return err
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
		columnDefs[i] = pq.QuoteIdentifier(col.Name) + " " + col.Type
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

func CreateSubscriptionTable(db *sql.DB, table PgTableDetail) error {
	tableColumns := createColumns(table.Columns)
	sql := fmt.Sprintf(`CREATE TABLE %s.%s (%s)`,
		pq.QuoteIdentifier(table.Schema), pq.QuoteIdentifier(table.Name), tableColumns)
	_, err := db.Exec(sql)
	return err
}

func RenameSubscriptionTable(db *sql.DB, table, newTable PgTable) error {
	sql := fmt.Sprintf(`ALTER TABLE IF EXISTS %s.%s RENAME TO %s`,
		pq.QuoteIdentifier(table.Schema), pq.QuoteIdentifier(table.Name),
		pq.QuoteIdentifier(newTable.Name))
	_, err := db.Exec(sql)
	return err
}

func CheckSubscriptionTableDetail(db *sql.DB, table PgTableDetail) error {
	subscriptionTable, err := tableColumns(db, PgTable{Schema: table.Schema, Name: table.Name}, false)
	if err != nil {
		return err
	}

	if !equalColumns(table.Columns, subscriptionTable.Columns) {
		return ErrWrongAttributes
	}
	return nil
}
