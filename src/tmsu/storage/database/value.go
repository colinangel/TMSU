/*
Copyright 2011-2015 Paul Ruane.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package database

import (
	"database/sql"
	"strings"
	"tmsu/entities"
)

// Retrieves the count of values.
func (db *Database) ValueCount() (uint, error) {
	sql := `SELECT count(1)
            FROM value`

	rows, err := db.ExecQuery(sql)
	if err != nil {
		return 0, err
	}

	return readCount(rows)
}

// Retrieves the complete set of values.
func (db *Database) Values() (entities.Values, error) {
	sql := `SELECT id, name
            FROM value
            ORDER BY name`

	rows, err := db.ExecQuery(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readValues(rows, make(entities.Values, 0, 10))
}

// Retrieves a specific value.
func (db *Database) Value(id entities.ValueId) (*entities.Value, error) {
	sql := `SELECT id, name
	        FROM value
	        WHERE id = ?`

	rows, err := db.ExecQuery(sql, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readValue(rows)
}

// Retrieves a specific set of values.
func (db *Database) ValuesByIds(ids entities.ValueIds) (entities.Values, error) {
	sql := `SELECT id, name
	        FROM value
	        WHERE id IN (?`
	sql += strings.Repeat(",?", len(ids)-1)
	sql += ")"

	params := make([]interface{}, len(ids))
	for index, id := range ids {
		params[index] = id
	}

	rows, err := db.ExecQuery(sql, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tags, err := readValues(rows, make(entities.Values, 0, len(ids)))
	if err != nil {
		return nil, err
	}

	return tags, nil
}

// Retrieves the set of unused values.
func (db *Database) UnusedValues() (entities.Values, error) {
	sql := `SELECT id, name
            FROM value
            WHERE id NOT IN (SELECT distinct(value_id)
                             FROM file_tag)`

	rows, err := db.ExecQuery(sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readValues(rows, make(entities.Values, 0, 10))
}

// Retrieves a specific value by name.
func (db *Database) ValueByName(name string) (*entities.Value, error) {
	sql := `SELECT id, name
	        FROM value
	        WHERE name = ?`

	rows, err := db.ExecQuery(sql, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readValue(rows)
}

// Retrieves the set of values with the specified names.
func (db *Database) ValuesByNames(names []string) (entities.Values, error) {
	if len(names) == 0 {
		return make(entities.Values, 0), nil
	}

	sql := `SELECT id, name
            FROM value
            WHERE name IN (?`
	sql += strings.Repeat(",?", len(names)-1)
	sql += ")"

	params := make([]interface{}, len(names))
	for index, name := range names {
		params[index] = name
	}

	rows, err := db.ExecQuery(sql, params...)
	if err != nil {
		return nil, err
	}

	values, err := readValues(rows, make(entities.Values, 0, len(names)))
	if err != nil {
		return nil, err
	}

	return values, nil
}

// Retrieves the set of values for the specified tag.
func (db *Database) ValuesByTagId(tagId entities.TagId) (entities.Values, error) {
	sql := `SELECT id, name
            FROM value
            WHERE id IN (
                SELECT value_id
                FROM file_tag
                WHERE tag_id = ?1)
            ORDER BY name`

	rows, err := db.ExecQuery(sql, tagId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return readValues(rows, make(entities.Values, 0, 10))
}

// Adds a value.
func (db *Database) InsertValue(name string) (*entities.Value, error) {
	sql := `INSERT INTO value (name)
	        VALUES (?)`

	result, err := db.Exec(sql, name)
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if rowsAffected != 1 {
		panic("expected exactly one row to be affected.")
	}

	return &entities.Value{entities.ValueId(id), name}, nil
}

// Deletes a value.
func (db *Database) DeleteValue(valueId entities.ValueId) error {
	sql := `DELETE FROM value
	        WHERE id = ?`

	result, err := db.Exec(sql, valueId)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected == 0 {
		return NoSuchValueError{valueId}
	}
	if rowsAffected > 1 {
		panic("expected only one row to be affected.")
	}

	return nil
}

// Deletes all unused values.
func (db *Database) DeleteUnusedValues(valueIds entities.ValueIds) error {
	if len(valueIds) == 0 {
		return nil
	}

	sql := `DELETE FROM value
            WHERE id IN (?`
	sql += strings.Repeat(",?", len(valueIds)-1)
	sql += `)
            AND id NOT IN (SELECT distinct(value_id)
                           FROM file_tag
                           WHERE id IN (?`
	sql += strings.Repeat(",?", len(valueIds)-1)
	sql += "))"

	params := make([]interface{}, len(valueIds)*2)
	for index, valueId := range valueIds {
		params[index] = valueId
		params[len(valueIds)+index] = valueId
	}

	_, err := db.Exec(sql, params...)
	if err != nil {
		return nil
	}

	return nil
}

// unexported

func readValue(rows *sql.Rows) (*entities.Value, error) {
	if !rows.Next() {
		return nil, nil
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}

	var id entities.ValueId
	var name string
	err := rows.Scan(&id, &name)
	if err != nil {
		return nil, err
	}

	return &entities.Value{id, name}, nil
}

func readValues(rows *sql.Rows, values entities.Values) (entities.Values, error) {
	for {
		value, err := readValue(rows)
		if err != nil {
			return nil, err
		}
		if value == nil {
			break
		}

		values = append(values, value)
	}

	return values, nil
}
