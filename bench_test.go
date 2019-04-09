package main

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	gopg "github.com/go-pg/pg"
	"github.com/jackc/go_db_bench/raw"
	"github.com/jackc/pgx"
	"github.com/jackc/pgx/pgtype"
)

var (
	setupOnce     sync.Once
	pgxPool       *pgx.ConnPool
	pgxStdlib     *sql.DB
	pq            *sql.DB
	pg            *gopg.DB
	rawConn       *raw.Conn
	randPersonIDs []int32
)

var selectPersonNameSQL = `select first_name from person where id=$1`
var selectPersonNameSQLQuestionMark = `select first_name from person where id=?`

var selectPersonSQL = `
select id, first_name, last_name, sex, birth_date, weight, height, update_time
from person
where id=$1`
var selectPersonSQLQuestionMark = `
select id, first_name, last_name, sex, birth_date, weight, height, update_time
from person
where id=?`

var selectMultiplePeopleSQL = `
select id, first_name, last_name, sex, birth_date, weight, height, update_time
from person
where id between $1 and $1 + 24`
var selectMultiplePeopleSQLQuestionMark = `
select id, first_name, last_name, sex, birth_date, weight, height, update_time
from person
where id between ? and ? + 24`

var selectLargeTextSQL = `select repeat('*', $1)`

var rawSelectPersonNameStmt *raw.PreparedStatement
var rawSelectPersonStmt *raw.PreparedStatement
var rawSelectMultiplePeopleStmt *raw.PreparedStatement
var rawSelectLargeTextStmt *raw.PreparedStatement

var rxBuf []byte

type person struct {
	TableName  struct{} `sql:"person"` // custom table name
	Id         int32
	FirstName  string    `sql:"first_name"`
	LastName   string    `sql:"last_name"`
	Sex        string    `sql:"sex"`
	BirthDate  time.Time `sql:"birth_date"`
	Weight     int32     `sql:"weight"`
	Height     int32     `sql:"height"`
	UpdateTime time.Time `sql:"update_time"`
}

type personBytes struct {
	Id         int32
	FirstName  []byte
	LastName   []byte
	Sex        []byte
	BirthDate  time.Time
	Weight     int32
	Height     int32
	UpdateTime time.Time
}

type People struct {
	tableName struct{} `pg:",discard_unknown_columns"`
	C         []person
}

func (people *People) NewRecord() interface{} {
	people.C = append(people.C, person{})
	return &people.C[len(people.C)-1]
}

var gopg_db *gopg.DB

func setup(b *testing.B) {
	setupOnce.Do(func() {
		config, err := extractConfig()
		if err != nil {
			b.Fatalf("extractConfig failed: %v", err)
		}

		config.AfterConnect = func(conn *pgx.Conn) error {
			_, err := conn.Prepare("selectPersonName", selectPersonNameSQL)
			if err != nil {
				return err
			}

			_, err = conn.Prepare("selectPerson", selectPersonSQL)
			if err != nil {
				return err
			}

			_, err = conn.Prepare("selectMultiplePeople", selectMultiplePeopleSQL)
			if err != nil {
				return err
			}

			_, err = conn.Prepare("selectLargeText", selectLargeTextSQL)
			if err != nil {
				return err
			}

			return nil
		}

		err = loadTestData(config)
		if err != nil {
			b.Fatalf("loadTestData failed: %v", err)
		}

		pgxPool, err = openPgxNative(config)
		if err != nil {
			b.Fatalf("openPgxNative failed: %v", err)
		}

		pgxStdlib, err = openPgxStdlib(config.ConnConfig)
		if err != nil {
			b.Fatalf("openPgxNative failed: %v", err)
		}

		pq, err = openPq(config)
		if err != nil {
			b.Fatalf("openPq failed: %v", err)
		}

		pg, err = openPg(config)
		if err != nil {
			b.Fatalf("openPg failed: %v", err)
		}

		gopg_db = pg

		rawConfig := raw.ConnConfig{
			Host:     config.Host,
			Port:     config.Port,
			User:     config.User,
			Password: config.Password,
			Database: config.Database,
		}
		rawConn, err = raw.Connect(rawConfig)
		if err != nil {
			b.Fatalf("raw.Connect failed: %v", err)
		}
		rawSelectPersonNameStmt, err = rawConn.Prepare("selectPersonName", selectPersonNameSQL)
		if err != nil {
			b.Fatalf("rawConn.Prepare failed: %v", err)
		}
		rawSelectPersonStmt, err = rawConn.Prepare("selectPerson", selectPersonSQL)
		if err != nil {
			b.Fatalf("rawConn.Prepare failed: %v", err)
		}
		rawSelectMultiplePeopleStmt, err = rawConn.Prepare("selectMultiplePeople", selectMultiplePeopleSQL)
		if err != nil {
			b.Fatalf("rawConn.Prepare failed: %v", err)
		}
		rawSelectMultiplePeopleStmt, err = rawConn.Prepare("selectLargeText", selectLargeTextSQL)
		if err != nil {
			b.Fatalf("rawConn.Prepare failed: %v", err)
		}

		rxBuf = make([]byte, 16384)

		// Get random person ids in random order outside of timing
		rows, _ := pgxPool.Query("select id from person order by random()")
		for rows.Next() {
			var id int32
			rows.Scan(&id)
			randPersonIDs = append(randPersonIDs, id)
		}

		if rows.Err() != nil {
			b.Fatalf("pgxPool.Query failed: %v", err)
		}
	})
}

func BenchmarkPgxNativeSelectSingleShortString(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		var firstName string
		err := pgxPool.QueryRow("selectPersonName", id).Scan(&firstName)
		if err != nil {
			b.Fatalf("pgxPool.QueryRow Scan failed: %v", err)
		}
		if len(firstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}

func BenchmarkPgxStdlibSelectSingleShortString(b *testing.B) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectPersonNameSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleShortString(b, stmt)
}

func BenchmarkPgSelectSingleShortString(b *testing.B) {
	setup(b)

	stmt, err := pg.Prepare(selectPersonNameSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var firstName string
		id := randPersonIDs[i%len(randPersonIDs)]
		_, err := stmt.QueryOne(gopg.Scan(&firstName), id)
		if err != nil {
			b.Fatalf("stmt.QueryOne failed: %v", err)
		}
		if len(firstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}
func BenchmarkPgModelsSelectSingleShortString(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		p := &person{}
		err := gopg_db.Model(p).Column("first_name").Where("id = ?", id).Select()
		if err != nil {
			b.Fatalf("db model QueryOne failed: %v", err)
		}
		if len(p.FirstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}

func BenchmarkPqSelectSingleShortString(b *testing.B) {
	setup(b)
	stmt, err := pq.Prepare(selectPersonNameSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleShortString(b, stmt)
}

func benchmarkSelectSingleShortString(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		row := stmt.QueryRow(id)
		var firstName string
		err := row.Scan(&firstName)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(firstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}

func BenchmarkRawSelectSingleShortValue(b *testing.B) {
	setup(b)

	b.ResetTimer()

	txBufs := make([][]byte, len(randPersonIDs))
	for i, personID := range randPersonIDs {
		var err error
		txBufs[i], err = rawConn.BuildPreparedQueryBuf(rawSelectPersonNameStmt, personID)
		if err != nil {
			b.Fatalf("rawConn.BuildQueryBuf failed: %v", err)
		}
	}

	for i := 0; i < b.N; i++ {
		txBuf := txBufs[i%len(txBufs)]
		_, err := rawConn.Conn().Write(txBuf)
		if err != nil {
			b.Fatalf("rawConn.Conn.Write failed: %v", err)
		}

		rxRawUntilReady(b)
	}
}

func BenchmarkPgxNativeSelectSingleShortBytes(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		var firstName []byte
		err := pgxPool.QueryRow("selectPersonName", id).Scan(&firstName)
		if err != nil {
			b.Fatalf("pgxPool.QueryRow Scan failed: %v", err)
		}
		if len(firstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}

func BenchmarkPgxStdlibSelectSingleShortBytes(b *testing.B) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectPersonNameSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleShortBytes(b, stmt)
}

func BenchmarkPqSelectSingleShortBytes(b *testing.B) {
	setup(b)
	stmt, err := pq.Prepare(selectPersonNameSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleShortBytes(b, stmt)
}

func benchmarkSelectSingleShortBytes(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		row := stmt.QueryRow(id)
		var firstName []byte
		err := row.Scan(&firstName)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(firstName) == 0 {
			b.Fatal("FirstName was empty")
		}
	}
}

func checkPersonWasFilled(b *testing.B, p person) {
	if p.Id == 0 {
		b.Fatal("id was 0")
	}
	if len(p.FirstName) == 0 {
		b.Fatal("FirstName was empty")
	}
	if len(p.LastName) == 0 {
		b.Fatal("LastName was empty")
	}
	if len(p.Sex) == 0 {
		b.Fatal("Sex was empty")
	}
	var zeroTime time.Time
	if p.BirthDate == zeroTime {
		b.Fatal("BirthDate was zero time")
	}
	if p.Weight == 0 {
		b.Fatal("Weight was 0")
	}
	if p.Height == 0 {
		b.Fatal("Height was 0")
	}
	if p.UpdateTime == zeroTime {
		b.Fatal("UpdateTime was zero time")
	}
}

func BenchmarkPgxNativeSelectSingleRow(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var p person
		id := randPersonIDs[i%len(randPersonIDs)]

		rows, _ := pgxPool.Query("selectPerson", id)
		for rows.Next() {
			rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
		}
		if rows.Err() != nil {
			b.Fatalf("pgxPool.Query failed: %v", rows.Err())
		}

		checkPersonWasFilled(b, p)
	}
}

func BenchmarkPgxStdlibSelectSingleRow(b *testing.B) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectPersonSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleRow(b, stmt)
}

func BenchmarkPgSelectSingleRow(b *testing.B) {
	setup(b)

	stmt, err := pg.Prepare(selectPersonSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var p person
		id := randPersonIDs[i%len(randPersonIDs)]
		_, err := stmt.QueryOne(&p, id)
		if err != nil {
			b.Fatalf("stmt.QueryOne failed: %v", err)
		}

		checkPersonWasFilled(b, p)
	}
}

func BenchmarkPgModelsSelectSingleRow(b *testing.B) {
	setup(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		p := person{}
		err := gopg_db.Model(&p).Where("id = ?", id).Select()
		if err != nil {
			b.Fatalf("db model QueryOne failed: %v", err)
		}
		checkPersonWasFilled(b, p)
	}
}

func BenchmarkPqSelectSingleRow(b *testing.B) {
	setup(b)
	stmt, err := pq.Prepare(selectPersonSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectSingleRow(b, stmt)
}

func benchmarkSelectSingleRow(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		row := stmt.QueryRow(id)
		var p person
		err := row.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}

		checkPersonWasFilled(b, p)
	}
}

func BenchmarkRawSelectSingleRow(b *testing.B) {
	setup(b)
	b.ResetTimer()

	txBufs := make([][]byte, len(randPersonIDs))
	for i, personID := range randPersonIDs {
		var err error
		txBufs[i], err = rawConn.BuildPreparedQueryBuf(rawSelectPersonStmt, personID)
		if err != nil {
			b.Fatalf("rawConn.BuildPreparedQueryBuf failed: %v", err)
		}
	}

	for i := 0; i < b.N; i++ {
		txBuf := txBufs[i%len(txBufs)]
		_, err := rawConn.Conn().Write(txBuf)
		if err != nil {
			b.Fatalf("rawConn.Conn.Write failed: %v", err)
		}

		rxRawUntilReady(b)
	}
}

func BenchmarkPgxNativeSelectMultipleRows(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]

		rows, _ := pgxPool.Query("selectMultiplePeople", id)
		var p person
		for rows.Next() {
			err := rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
			if err != nil {
				b.Fatalf("rows.Scan failed: %v", err)
			}
			checkPersonWasFilled(b, p)
		}
		if rows.Err() != nil {
			b.Fatalf("pgxPool.Query failed: %v", rows.Err())
		}
	}
}

func BenchmarkPgxNativeSelectMultipleRowsIntoGenericBinary(b *testing.B) {
	setup(b)

	type personRaw struct {
		Id         pgtype.GenericBinary
		FirstName  pgtype.GenericBinary
		LastName   pgtype.GenericBinary
		Sex        pgtype.GenericBinary
		BirthDate  pgtype.GenericBinary
		Weight     pgtype.GenericBinary
		Height     pgtype.GenericBinary
		UpdateTime pgtype.GenericBinary
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]

		rows, _ := pgxPool.Query("selectMultiplePeople", id)
		var p personRaw
		for rows.Next() {
			err := rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
			if err != nil {
				b.Fatalf("rows.Scan failed: %v", err)
			}
		}
		if rows.Err() != nil {
			b.Fatalf("pgxPool.Query failed: %v", rows.Err())
		}
	}
}

func BenchmarkPgxStdlibSelectMultipleRows(b *testing.B) {
	setup(b)

	stmt, err := pgxStdlib.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectMultipleRows(b, stmt)
}

// This benchmark is different than the other multiple rows in that it collects
// all rows where as the others process and discard. So it is not apples-to-
// apples for *SelectMultipleRows*.
func BenchmarkPgSelectMultipleRowsCollect(b *testing.B) {
	setup(b)

	stmt, err := pg.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var people People
		id := randPersonIDs[i%len(randPersonIDs)]
		_, err := stmt.Query(&people, id)
		if err != nil {
			b.Fatalf("stmt.Query failed: %v", err)
		}

		for i := range people.C {
			checkPersonWasFilled(b, people.C[i])
		}
	}
}

func BenchmarkPgModelsSelectMultipleRowsCollect(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]

		var persons []person
		err := gopg_db.Model(&persons).Where("id >= ?", id).Where("id <= ?", id+24).Select()
		if err != nil {
			b.Fatalf("db model QueryOne failed: %v", err)
		}
		for i := range persons {
			checkPersonWasFilled(b, persons[i])
		}
	}
}

func BenchmarkPgSelectMultipleRowsAndDiscard(b *testing.B) {
	setup(b)

	stmt, err := pg.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		_, err := stmt.Query(gopg.Discard, id)
		if err != nil {
			b.Fatalf("stmt.Query failed: %v", err)
		}
	}
}

func BenchmarkPqSelectMultipleRows(b *testing.B) {
	setup(b)

	stmt, err := pq.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectMultipleRows(b, stmt)
}

func benchmarkSelectMultipleRows(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		rows, err := stmt.Query(id)
		if err != nil {
			b.Fatalf("db.Query failed: %v", err)
		}

		var p person
		for rows.Next() {
			err := rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
			if err != nil {
				b.Fatalf("rows.Scan failed: %v", err)
			}
			checkPersonWasFilled(b, p)
		}

		if rows.Err() != nil {
			b.Fatalf("rows.Err() returned an error: %v", err)
		}
	}
}

func BenchmarkRawSelectMultipleRows(b *testing.B) {
	setup(b)

	b.ResetTimer()

	txBufs := make([][]byte, len(randPersonIDs))
	for i, personID := range randPersonIDs {
		var err error
		txBufs[i], err = rawConn.BuildPreparedQueryBuf(rawSelectMultiplePeopleStmt, personID)
		if err != nil {
			b.Fatalf("rawConn.BuildPreparedQueryBuf failed: %v", err)
		}
	}

	for i := 0; i < b.N; i++ {
		txBuf := txBufs[i%len(txBufs)]
		_, err := rawConn.Conn().Write(txBuf)
		if err != nil {
			b.Fatalf("rawConn.Conn.Write failed: %v", err)
		}

		rxRawUntilReady(b)
	}
}

func rxRawUntilReady(b *testing.B) {
	for {
		n, err := rawConn.Conn().Read(rxBuf)
		if err != nil {
			b.Fatalf("rawConn.Conn.Read failed: %v", err)
		}
		if rxBuf[n-6] == 'Z' && rxBuf[n-2] == 5 && rxBuf[n-1] == 'I' {
			return
		}
	}
}

func checkPersonBytesWasFilled(b *testing.B, p personBytes) {
	if p.Id == 0 {
		b.Fatal("id was 0")
	}
	if len(p.FirstName) == 0 {
		b.Fatal("FirstName was empty")
	}
	if len(p.LastName) == 0 {
		b.Fatal("LastName was empty")
	}
	if len(p.Sex) == 0 {
		b.Fatal("Sex was empty")
	}
	var zeroTime time.Time
	if p.BirthDate == zeroTime {
		b.Fatal("BirthDate was zero time")
	}
	if p.Weight == 0 {
		b.Fatal("Weight was 0")
	}
	if p.Height == 0 {
		b.Fatal("Height was 0")
	}
	if p.UpdateTime == zeroTime {
		b.Fatal("UpdateTime was zero time")
	}
}

func BenchmarkPgxNativeSelectMultipleRowsBytes(b *testing.B) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]

		rows, _ := pgxPool.Query("selectMultiplePeople", id)
		var p personBytes
		for rows.Next() {
			err := rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
			if err != nil {
				b.Fatalf("rows.Scan failed: %v", err)
			}
			checkPersonBytesWasFilled(b, p)
		}
		if rows.Err() != nil {
			b.Fatalf("pgxPool.Query failed: %v", rows.Err())
		}
	}
}

func BenchmarkPgxStdlibSelectMultipleRowsBytes(b *testing.B) {
	setup(b)

	stmt, err := pgxStdlib.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectMultipleRowsBytes(b, stmt)
}

func BenchmarkPqSelectMultipleRowsBytes(b *testing.B) {
	setup(b)

	stmt, err := pq.Prepare(selectMultiplePeopleSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectMultipleRowsBytes(b, stmt)
}

func benchmarkSelectMultipleRowsBytes(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		id := randPersonIDs[i%len(randPersonIDs)]
		rows, err := stmt.Query(id)
		if err != nil {
			b.Fatalf("db.Query failed: %v", err)
		}

		var p personBytes
		for rows.Next() {
			err := rows.Scan(&p.Id, &p.FirstName, &p.LastName, &p.Sex, &p.BirthDate, &p.Weight, &p.Height, &p.UpdateTime)
			if err != nil {
				b.Fatalf("rows.Scan failed: %v", err)
			}
			checkPersonBytesWasFilled(b, p)
		}

		if rows.Err() != nil {
			b.Fatalf("rows.Err() returned an error: %v", err)
		}
	}
}

func BenchmarkPgxNativeSelectBatch3Query(b *testing.B) {
	setup(b)

	b.ResetTimer()
	results := make([]string, 3)
	for i := 0; i < b.N; i++ {

		batch := pgxPool.BeginBatch()
		for j := range results {
			batch.Queue("selectLargeText", []interface{}{j}, nil, []int16{pgx.BinaryFormatCode})
		}

		if err := batch.Send(context.Background(), nil); err != nil {
			b.Fatal(err)
		}

		for j := range results {
			if err := batch.QueryRowResults().Scan(&results[j]); err != nil {
				b.Fatal(err)
			}
		}

		if err := batch.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPgxNativeSelectNoBatch3Query(b *testing.B) {
	setup(b)

	b.ResetTimer()
	results := make([]string, 3)
	for i := 0; i < b.N; i++ {
		for j := range results {
			if err := pgxPool.QueryRow("selectLargeText", j).Scan(&results[j]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPgxStdlibSelectNoBatch3Query(b *testing.B) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectNoBatch3Query(b, stmt)
}

func BenchmarkPqSelectNoBatch3Query(b *testing.B) {
	setup(b)
	stmt, err := pq.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectNoBatch3Query(b, stmt)
}

func benchmarkSelectNoBatch3Query(b *testing.B, stmt *sql.Stmt) {
	b.ResetTimer()
	results := make([]string, 3)
	for i := 0; i < b.N; i++ {
		for j := range results {
			if err := stmt.QueryRow(j).Scan(&results[j]); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkPgxNativeSelectLargeTextString1KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextString(b, 1024)
}

func BenchmarkPgxNativeSelectLargeTextString8KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextString(b, 8*1024)
}

func BenchmarkPgxNativeSelectLargeTextString64KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextString(b, 64*1024)
}

func BenchmarkPgxNativeSelectLargeTextString512KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextString(b, 512*1024)
}

func BenchmarkPgxNativeSelectLargeTextString4096KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextString(b, 4096*1024)
}

func benchmarkPgxNativeSelectLargeTextString(b *testing.B, size int) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s string
		err := pgxPool.QueryRow("selectLargeText", size).Scan(&s)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(s) != size {
			b.Fatalf("expected length %v, got %v", size, len(s))
		}
	}
}

func BenchmarkPgxStdlibSelectLargeTextString1KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextString(b, 1024)
}

func BenchmarkPgxStdlibSelectLargeTextString8KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextString(b, 8*1024)
}

func BenchmarkPgxStdlibSelectLargeTextString64KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextString(b, 64*1024)
}

func BenchmarkPgxStdlibSelectLargeTextString512KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextString(b, 512*1024)
}

func BenchmarkPgxStdlibSelectLargeTextString4096KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextString(b, 4096*1024)
}

func benchmarkPgxStdlibSelectLargeTextString(b *testing.B, size int) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectLargeTextString(b, stmt, size)
}

func BenchmarkGoPgSelectLargeTextString1KB(b *testing.B) {
	benchmarkPgSelectLargeTextString(b, 1024)
}

func BenchmarkGoPgSelectLargeTextString8KB(b *testing.B) {
	benchmarkPgSelectLargeTextString(b, 8*1024)
}

func BenchmarkGoPgSelectLargeTextString64KB(b *testing.B) {
	benchmarkPgSelectLargeTextString(b, 64*1024)
}

func BenchmarkGoPgSelectLargeTextString512KB(b *testing.B) {
	benchmarkPgSelectLargeTextString(b, 512*1024)
}

func BenchmarkGoPgSelectLargeTextString4096KB(b *testing.B) {
	benchmarkPgSelectLargeTextString(b, 4096*1024)
}

func benchmarkPgSelectLargeTextString(b *testing.B, size int) {
	setup(b)

	stmt, err := pg.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s string
		_, err := stmt.QueryOne(gopg.Scan(&s), size)
		if err != nil {
			b.Fatalf("stmt.QueryOne failed: %v", err)
		}
		if len(s) != size {
			b.Fatalf("expected length %v, got %v", size, len(s))
		}
	}
}

func BenchmarkPqSelectLargeTextString1KB(b *testing.B) {
	benchmarkPqSelectLargeTextString(b, 1024)
}

func BenchmarkPqSelectLargeTextString8KB(b *testing.B) {
	benchmarkPqSelectLargeTextString(b, 8*1024)
}

func BenchmarkPqSelectLargeTextString64KB(b *testing.B) {
	benchmarkPqSelectLargeTextString(b, 64*1024)
}

func BenchmarkPqSelectLargeTextString512KB(b *testing.B) {
	benchmarkPqSelectLargeTextString(b, 512*1024)
}

func BenchmarkPqSelectLargeTextString4096KB(b *testing.B) {
	benchmarkPqSelectLargeTextString(b, 4096*1024)
}

func benchmarkPqSelectLargeTextString(b *testing.B, size int) {
	setup(b)
	stmt, err := pq.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectLargeTextString(b, stmt, size)
}

func benchmarkSelectLargeTextString(b *testing.B, stmt *sql.Stmt, size int) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s string
		err := stmt.QueryRow(size).Scan(&s)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(s) != size {
			b.Fatalf("expected length %v, got %v", size, len(s))
		}
	}
}

func BenchmarkPgxNativeSelectLargeTextBytes1KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextBytes(b, 1024)
}

func BenchmarkPgxNativeSelectLargeTextBytes8KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextBytes(b, 8*1024)
}

func BenchmarkPgxNativeSelectLargeTextBytes64KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextBytes(b, 64*1024)
}

func BenchmarkPgxNativeSelectLargeTextBytes512KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextBytes(b, 512*1024)
}

func BenchmarkPgxNativeSelectLargeTextBytes4096KB(b *testing.B) {
	benchmarkPgxNativeSelectLargeTextBytes(b, 4096*1024)
}

func benchmarkPgxNativeSelectLargeTextBytes(b *testing.B, size int) {
	setup(b)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s []byte
		err := pgxPool.QueryRow("selectLargeText", size).Scan(&s)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(s) != size {
			b.Fatalf("expected length %v, got %v", size, len(s))
		}
	}
}

func BenchmarkPgxStdlibSelectLargeTextBytes1KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextBytes(b, 1024)
}

func BenchmarkPgxStdlibSelectLargeTextBytes8KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextBytes(b, 8*1024)
}

func BenchmarkPgxStdlibSelectLargeTextBytes64KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextBytes(b, 64*1024)
}

func BenchmarkPgxStdlibSelectLargeTextBytes512KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextBytes(b, 512*1024)
}

func BenchmarkPgxStdlibSelectLargeTextBytes4096KB(b *testing.B) {
	benchmarkPgxStdlibSelectLargeTextBytes(b, 4096*1024)
}

func benchmarkPgxStdlibSelectLargeTextBytes(b *testing.B, size int) {
	setup(b)
	stmt, err := pgxStdlib.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectLargeTextBytes(b, stmt, size)
}

func BenchmarkPqSelectLargeTextBytes1KB(b *testing.B) {
	benchmarkPqSelectLargeTextBytes(b, 1024)
}

func BenchmarkPqSelectLargeTextBytes8KB(b *testing.B) {
	benchmarkPqSelectLargeTextBytes(b, 8*1024)
}

func BenchmarkPqSelectLargeTextBytes64KB(b *testing.B) {
	benchmarkPqSelectLargeTextBytes(b, 64*1024)
}

func BenchmarkPqSelectLargeTextBytes512KB(b *testing.B) {
	benchmarkPqSelectLargeTextBytes(b, 512*1024)
}

func BenchmarkPqSelectLargeTextBytes4096KB(b *testing.B) {
	benchmarkPqSelectLargeTextBytes(b, 4096*1024)
}

func benchmarkPqSelectLargeTextBytes(b *testing.B, size int) {
	setup(b)
	stmt, err := pq.Prepare(selectLargeTextSQL)
	if err != nil {
		b.Fatalf("Prepare failed: %v", err)
	}
	defer stmt.Close()

	benchmarkSelectLargeTextBytes(b, stmt, size)
}

func benchmarkSelectLargeTextBytes(b *testing.B, stmt *sql.Stmt, size int) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var s []byte
		err := stmt.QueryRow(size).Scan(&s)
		if err != nil {
			b.Fatalf("row.Scan failed: %v", err)
		}
		if len(s) != size {
			b.Fatalf("expected length %v, got %v", size, len(s))
		}
	}
}
