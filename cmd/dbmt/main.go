// Command dbmt is a small DB-management CLI (DRAFT).
//
// Usage:
//
//	dbmt <operation> <table> [flags]
//
//	operations : select | insert | update | delete
//	tables     : users | routes | waypoints | note_sets | notes
//
// With no data flags, insert/update prompt for each column one by one. Or pass
// the data as JSON with --file:
//
//	dbmt insert routes --file route.json
//	dbmt select routes --id <uuid>
//	dbmt select notes --limit 20
//	dbmt update users --id <uuid> --file patch.json
//	dbmt delete waypoints --id <uuid> --yes
//
// Connection comes from --dsn or the DATABASE_CONNECTION_STRING env var.
//
// NOTE (draft): this is an admin tool. It works directly at the table level and
// deliberately BYPASSES the domain services and their validation — so it can
// fix/seed data the API would reject. It is NOT meant to be exposed to end
// users. Identifiers are whitelisted (the table registry) and all values are
// passed as query parameters, so user input cannot inject SQL.
//
// TODO: richer filters than --id (e.g. --where col=val), batch/CSV input,
// GeoJSON input for geometry columns (currently WKT), dry-run.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// column describes one table column for prompting and safe query building.
type column struct {
	name string
	// auto columns are server-assigned (id, timestamps, generated): never
	// prompted and never written.
	auto bool
	// optional columns may be left blank (omitted from insert/update).
	optional bool
	// sqlExpr is how a provided value (always passed as a text parameter) is
	// placed into INSERT/UPDATE SQL. %d is the parameter index. The default is a
	// plain text cast; geometry, arrays and enums override it.
	sqlExpr string
	// selectExpr is how the column is read back in SELECT (%s is the quoted
	// name). Geometry columns render as WKT.
	selectExpr string
	// hint is shown in the interactive prompt.
	hint string
}

type table struct {
	name    string
	columns []column
}

func col(name string, opts ...func(*column)) column {
	c := column{name: name, sqlExpr: "$%d::text", selectExpr: "%[1]s"}
	for _, o := range opts {
		o(&c)
	}
	return c
}

func auto() func(*column)         { return func(c *column) { c.auto = true } }
func optional() func(*column)     { return func(c *column) { c.optional = true } }
func expr(s string) func(*column) { return func(c *column) { c.sqlExpr = s } }
func hint(s string) func(*column) { return func(c *column) { c.hint = s } }
func geometry() func(*column) {
	return func(c *column) {
		c.sqlExpr = "ST_GeomFromText($%d, 4326)"
		c.selectExpr = "ST_AsText(%[1]s) AS %[1]s"
		c.hint = "WKT, e.g. POINT(9.19 45.46)"
	}
}

// registry is the whitelist of tables the tool can touch.
var registry = map[string]table{
	"users": {name: "users", columns: []column{
		col("id", auto()),
		col("keycloak_sub", hint("Keycloak subject")),
		col("display_name"),
		col("description", optional()),
		col("avatar_content_type", optional(), hint("e.g. image/png")),
		col("created_at", auto()),
		col("updated_at", auto()),
	}},
	"routes": {name: "routes", columns: []column{
		col("id", auto()),
		col("author_id", optional(), expr("$%d::uuid")),
		col("name", optional()),
		col("description", optional()),
		col("path", geometry()),
		col("length_m", auto()), // generated
		col("start_city"),
		col("finish_city"),
		col("vehicles", expr("$%d::vehicle_type[]"), hint("{CAR,BIKE}")),
		col("created_at", auto()),
		col("updated_at", auto()),
	}},
	"waypoints": {name: "waypoints", columns: []column{
		col("id", auto()),
		col("route_id", expr("$%d::uuid")),
		col("position", geometry()),
		col("order", expr("$%d::int")),
		col("name", optional()),
	}},
	"note_sets": {name: "note_sets", columns: []column{
		col("id", auto()),
		col("route_id", expr("$%d::uuid")),
		col("author_id", optional(), expr("$%d::uuid")),
		col("name", optional()),
		col("created_at", auto()),
		col("updated_at", auto()),
	}},
	"notes": {name: "notes", columns: []column{
		col("id", auto()),
		col("set_id", expr("$%d::uuid")),
		col("position", geometry()),
		col("order", expr("$%d::int")),
		col("type", expr("$%d::note_type"), hint("INDICATION|WARNING")),
		col("direction", optional(), expr("$%d::direction_type"), hint("LEFT|RIGHT|STRAIGHT|CHICANE")),
		col("severity", optional(), expr("$%d::int"), hint("1-7")),
		col("description", optional()),
	}},
}

// flags holds the parsed command-line options.
type flags struct {
	id      string
	file    string
	limit   int
	yes     bool
	dsnFlag string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "dbmt:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: dbmt <select|insert|update|delete> <table> [flags]")
	}
	op, tableName := args[0], args[1]

	tbl, ok := registry[tableName]
	if !ok {
		return fmt.Errorf("unknown table %q (known: %s)", tableName, strings.Join(tableNames(), ", "))
	}

	f, err := parseFlags(args[2:])
	if err != nil {
		return err
	}

	dsn := f.dsn()
	if dsn == "" {
		return fmt.Errorf("no DSN: set --dsn or DATABASE_CONNECTION_STRING")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close(ctx)

	switch op {
	case "select":
		return doSelect(ctx, conn, tbl, f)
	case "insert":
		return doInsert(ctx, conn, tbl, f)
	case "update":
		return doUpdate(ctx, conn, tbl, f)
	case "delete":
		return doDelete(ctx, conn, tbl, f)
	default:
		return fmt.Errorf("unknown operation %q (use select|insert|update|delete)", op)
	}
}

// ----- operations -----------------------------------------------------------

func doSelect(ctx context.Context, conn *pgx.Conn, tbl table, f flags) error {
	exprs := make([]string, 0, len(tbl.columns))
	for _, c := range tbl.columns {
		exprs = append(exprs, fmt.Sprintf(c.selectExpr, quote(c.name)))
	}
	sql := fmt.Sprintf(`SELECT %s FROM %s`, strings.Join(exprs, ", "), quote(tbl.name))

	var args []any
	switch {
	case f.id != "":
		sql += ` WHERE "id" = $1::uuid`
		args = append(args, f.id)
	default:
		limit := f.limit
		if limit <= 0 {
			limit = 50
		}
		sql += fmt.Sprintf(" LIMIT %d", limit)
	}

	rows, err := conn.Query(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("select: %w", err)
	}
	defer rows.Close()

	fields := rows.FieldDescriptions()
	n := 0
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return err
		}
		row := make(map[string]any, len(vals))
		for i, fd := range fields {
			row[string(fd.Name)] = renderValue(vals[i])
		}
		out, _ := json.MarshalIndent(row, "", "  ")
		fmt.Println(string(out))
		n++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "(%d row(s))\n", n)
	return nil
}

func doInsert(ctx context.Context, conn *pgx.Conn, tbl table, f flags) error {
	values, err := gatherValues(tbl, f, false)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("nothing to insert")
	}

	var cols, placeholders []string
	var args []any
	for _, kv := range values {
		args = append(args, kv.value)
		cols = append(cols, quote(kv.col.name))
		placeholders = append(placeholders, fmt.Sprintf(kv.col.sqlExpr, len(args)))
	}

	sql := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) RETURNING "id"`,
		quote(tbl.name), strings.Join(cols, ", "), strings.Join(placeholders, ", "))

	var id any
	if err := conn.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	fmt.Printf("inserted %v\n", renderValue(id))
	return nil
}

func doUpdate(ctx context.Context, conn *pgx.Conn, tbl table, f flags) error {
	if f.id == "" {
		return fmt.Errorf("update requires --id <uuid>")
	}
	values, err := gatherValues(tbl, f, true)
	if err != nil {
		return err
	}
	if len(values) == 0 {
		return fmt.Errorf("nothing to update")
	}

	var sets []string
	var args []any
	for _, kv := range values {
		args = append(args, kv.value)
		sets = append(sets, fmt.Sprintf(`%s = %s`, quote(kv.col.name), fmt.Sprintf(kv.col.sqlExpr, len(args))))
	}
	args = append(args, f.id)
	sql := fmt.Sprintf(`UPDATE %s SET %s WHERE "id" = $%d::uuid`,
		quote(tbl.name), strings.Join(sets, ", "), len(args))

	tag, err := conn.Exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("update: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no row with id %s", f.id)
	}
	fmt.Printf("updated %d row(s)\n", tag.RowsAffected())
	return nil
}

func doDelete(ctx context.Context, conn *pgx.Conn, tbl table, f flags) error {
	if f.id == "" {
		return fmt.Errorf("delete requires --id <uuid> (refusing to delete the whole table)")
	}
	if !f.yes && !confirm(fmt.Sprintf("Delete row %s from %s?", f.id, tbl.name)) {
		fmt.Println("aborted")
		return nil
	}
	tag, err := conn.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE "id" = $1::uuid`, quote(tbl.name)), f.id)
	if err != nil {
		return fmt.Errorf("delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no row with id %s", f.id)
	}
	fmt.Printf("deleted %d row(s)\n", tag.RowsAffected())
	return nil
}

// ----- value gathering ------------------------------------------------------

type colValue struct {
	col   column
	value string
}

// gatherValues collects the column values to write, either from a JSON --file
// or by prompting the user one column at a time. forUpdate skips columns left
// blank (so an update only touches the fields the user supplied).
func gatherValues(tbl table, f flags, forUpdate bool) ([]colValue, error) {
	provided := map[string]string{}

	if f.file != "" {
		raw, err := os.ReadFile(f.file)
		if err != nil {
			return nil, err
		}
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f.file, err)
		}
		for k, v := range m {
			s, err := toSQLText(v)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", k, err)
			}
			provided[k] = s
		}
	} else {
		reader := bufio.NewReader(os.Stdin)
		for _, c := range tbl.columns {
			if c.auto {
				continue
			}
			label := c.name
			if c.hint != "" {
				label += " (" + c.hint + ")"
			}
			if c.optional || forUpdate {
				label += " [blank to skip]"
			}
			fmt.Printf("%s: ", label)
			line, _ := reader.ReadString('\n')
			line = strings.TrimRight(line, "\r\n")
			if line != "" {
				provided[c.name] = line
			}
		}
	}

	// Map provided values onto known, writable columns (ignoring auto columns).
	var out []colValue
	for _, c := range tbl.columns {
		if c.auto {
			continue
		}
		if v, ok := provided[c.name]; ok {
			out = append(out, colValue{col: c, value: v})
		}
	}
	return out, nil
}

// toSQLText renders a JSON value as the text form the column cast expects.
func toSQLText(v any) (string, error) {
	switch t := v.(type) {
	case nil:
		return "", fmt.Errorf("null not supported (omit the field instead)")
	case string:
		return t, nil
	case bool:
		return fmt.Sprintf("%t", t), nil
	case float64:
		// JSON numbers are float64; render ints without a trailing .0.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t)), nil
		}
		return fmt.Sprintf("%g", t), nil
	case []any:
		// Postgres array literal, e.g. ["CAR","BIKE"] → {CAR,BIKE}.
		parts := make([]string, len(t))
		for i, e := range t {
			s, err := toSQLText(e)
			if err != nil {
				return "", err
			}
			parts[i] = s
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("unsupported JSON type %T", v)
	}
}

// ----- helpers --------------------------------------------------------------

func parseFlags(args []string) (flags, error) {
	var f flags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--id":
			i++
			f.id = value(args, i)
		case "--file":
			i++
			f.file = value(args, i)
		case "--limit":
			i++
			fmt.Sscanf(value(args, i), "%d", &f.limit)
		case "--dsn":
			i++
			f.dsnFlag = value(args, i)
		case "--yes", "-y":
			f.yes = true
		default:
			return f, fmt.Errorf("unknown flag %q", args[i])
		}
	}
	return f, nil
}

func value(args []string, i int) string {
	if i >= len(args) {
		return ""
	}
	return args[i]
}

func (f flags) dsn() string {
	if f.dsnFlag != "" {
		return f.dsnFlag
	}
	return os.Getenv("DATABASE_CONNECTION_STRING")
}

func confirm(prompt string) bool {
	fmt.Printf("%s [y/N] ", prompt)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// renderValue makes a scanned value JSON-friendly: pgx returns UUIDs as raw
// [16]byte, which would print as a byte array.
func renderValue(v any) any {
	if b, ok := v.([16]byte); ok {
		return uuid.UUID(b).String()
	}
	return v
}

// quote wraps an identifier in double quotes so reserved words like "order" are
// safe. Identifiers only ever come from the registry, never from user input.
func quote(ident string) string { return `"` + ident + `"` }

func tableNames() []string {
	names := make([]string, 0, len(registry))
	for n := range registry {
		names = append(names, n)
	}
	return names
}
