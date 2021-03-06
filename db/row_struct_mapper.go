//    Copyright (C) 2016  mparaiso <mparaiso@online.fr>
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

package db

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/Mparaiso/go-tiger/logger"
)

// RowsScanner can scan a db row and iterate over db rows
type RowsScanner interface {
	Close() error
	Columns() ([]string, error)
	Err() error
	Next() bool
	Scan(destination ...interface{}) error
}

// Scanner populates destination values
// or returns an error
type Scanner interface {
	Scan(destination ...interface{}) error
}

// MapRowsToSliceOfSlices maps db rows to a slice of slices
func MapRowsToSliceOfSlices(scanner RowsScanner, Slices *[][]interface{}) error {
	defer scanner.Close()
	for scanner.Next() {
		columns, err := scanner.Columns()
		if err != nil {
			return err
		}
		sliceOfResults := make([]interface{}, len(columns))
		for i := range columns {
			// @see https://github.com/jmoiron/sqlx/blob/398dd5876282499cdfd4cb8ea0f31a672abe9495/sqlx.go#L751
			// create a new interface{} than is not nil
			sliceOfResults[i] = new(interface{})
		}
		err = scanner.Scan(sliceOfResults...)
		if err != nil {
			return err
		}
		row := []interface{}{}
		for index := range columns {
			// @see https://github.com/jmoiron/sqlx/blob/398dd5876282499cdfd4cb8ea0f31a672abe9495/sqlx.go#L751
			// convert the sliceOfResults[index] value back to interface{}
			var v interface{} = *(sliceOfResults[index].(*interface{}))
			if u, ok := v.([]uint8); ok {
				// v is likely a string, so convert to string
				row = append(row, (interface{}(string(u))))
			} else {
				row = append(row, v)
			}
		}
		*Slices = append(*Slices, row)
	}
	return scanner.Err()
}

// MapRowsToSliceOfMaps maps db rows to maps
// the map keys are the column names (or the aliases if defined in the query)
func MapRowsToSliceOfMaps(scanner RowsScanner, Map *[]map[string]interface{}) error {
	defer scanner.Close()
	for scanner.Next() {
		columns, err := scanner.Columns()
		if err != nil {
			return err
		}
		sliceOfResults := make([]interface{}, len(columns))
		for i := range columns {
			// @see https://github.com/jmoiron/sqlx/blob/398dd5876282499cdfd4cb8ea0f31a672abe9495/sqlx.go#L751
			// create a new interface{} than is not nil
			sliceOfResults[i] = new(interface{})
		}
		err = scanner.Scan(sliceOfResults...)
		if err != nil {
			return err
		}
		row := map[string]interface{}{}
		for index, column := range columns {
			// @see https://github.com/jmoiron/sqlx/blob/398dd5876282499cdfd4cb8ea0f31a672abe9495/sqlx.go#L751
			// convert the sliceOfResults[index] value back to interface{}
			var v interface{} = *(sliceOfResults[index].(*interface{}))
			if u, ok := v.([]uint8); ok {
				// v is likely a string, so convert to string
				row[column] = (interface{}(string(u)))
			} else {
				row[column] = v
			}
		}
		*Map = append(*Map, row)
	}
	return scanner.Err()
}

// MapRowsToSliceOfStruct  maps db rows to structs
func MapRowsToSliceOfStruct(scanner RowsScanner, pointerToASliceOfStructs interface{}, ignoreMissingField bool, transforms ...func(string) string) error {
	defer scanner.Close()
	recordsPointerValue := reflect.ValueOf(pointerToASliceOfStructs)
	if recordsPointerValue.Kind() != reflect.Ptr {
		return fmt.Errorf("Expect pointer, got %#v", pointerToASliceOfStructs)
	}
	recordsValue := recordsPointerValue.Elem()
	if recordsValue.Kind() != reflect.Slice {
		return fmt.Errorf("The underlying type is not a slice,pointer to slice expected for %#v ", recordsValue)
	}

	columns, err := scanner.Columns()
	if err != nil {
		return err
	}

	// get the underlying type of a slice
	// @see http://stackoverflow.com/questions/24366895/golang-reflect-slice-underlying-type
	for scanner.Next() {
		//
		var t reflect.Type
		if recordsValue.Type().Elem().Kind() == reflect.Ptr {
			// the sliceOfStructs type is like []*T
			t = recordsValue.Type().Elem().Elem()
		} else {
			// the sliceOfStructs type is like []T
			t = recordsValue.Type().Elem()
		}
		pointerOfElement := reflect.New(t)
		if len(transforms) == 0 {
			// a default transform will be added , and will correlate the name of a field with a sql struct tag
			tagMapper, err := CreateTagMapperFunc(pointerOfElement.Interface())
			if err != nil {
				return err
			}
			transforms = []func(string) string{tagMapper}
		}
		err = MapRowToStruct(columns, scanner, pointerOfElement.Interface(), ignoreMissingField, transforms...)
		if err != nil {
			return err
		}
		recordsValue = reflect.Append(recordsValue, pointerOfElement)
	}
	recordsPointerValue.Elem().Set(recordsValue)
	return scanner.Err()
}

// MapRowToStruct  automatically maps a db row to a struct.
//
// columns are the names of the columns in the row, they should match the fieldnames of Struct unless an optional transform function
// is passed.
//
// scanner is the Scanner (a sql.Row type for instance).
//
// Struct is a pointer to the struct that needs to be populated by the row data.
//
// ignoreMissingFields will ignore missing fields if the number of columns in the row doesn't match the number of fields in the struct.
//
// transforms is an optinal function that changes the name of the columns to match the name of the fields.
func MapRowToStruct(columns []string, scanner Scanner, Struct interface{}, ignoreMissingFields bool, transforms ...func(string) string) error {
	if len(transforms) == 0 {
		transforms = []func(string) string{noop}
	}
	structPointer := reflect.ValueOf(Struct)
	if structPointer.Kind() != reflect.Ptr {
		return fmt.Errorf("Pointer expected, got %#v", Struct)
	}
	structValue := reflect.Indirect(structPointer)
	zeroValue := reflect.Value{}
	arrayOfResults := []interface{}{}
	for _, column := range columns {
		column = transforms[0](column)
		field := structValue.FieldByName(column)
		if field == zeroValue {
			if ignoreMissingFields {
				pointer := reflect.New(reflect.TypeOf([]byte{}))
				arrayOfResults = append(arrayOfResults, pointer.Interface())
			} else {
				return fmt.Errorf("No field found for column %s in struct %#v", column, Struct)

			}
		} else {
			if !field.CanSet() {
				return fmt.Errorf("Unexported field %s cannot be set in struct %#v", column, Struct)
			}
			arrayOfResults = append(arrayOfResults, field.Addr().Interface())
		}
	}
	err := scanner.Scan(arrayOfResults...)
	if err != nil {
		return err
	}
	return nil
}

func noop(s string) string { return s }

// CreateTagMapperFunc creates a function that
// can be used to map db fields to struct fields
// through the use of struct tags.
//
// For instance :
//
//      type Foo struct{
//         Bar `sql:"bar"`
//      }
//
//      foo := new(Foo)
//      tagMapper := CreateTagMapperFunc(Foo{})
//      err := MapRowToStruct([]string{"bar"},someRow,foo,true,tagMapper)
//
// Will map Bar field in struct to bar DB field in the row
func CreateTagMapperFunc(Struct interface{}, tagname ...string) (func(string) string, error) {
	structValue := reflect.Indirect(reflect.ValueOf(Struct))
	if structValue.Kind() != reflect.Struct {
		return nil, fmt.Errorf("Struct expected, got %#v", Struct)
	}
	if len(tagname) == 0 {
		tagname = []string{"sql"}
	}
	m := map[string]string{}
	for i := 0; i < structValue.NumField(); i++ {
		name := structValue.Type().Field(i).Name
		// There can be multiple tags in the struct tag, always only check the first tag
		stringTags := structValue.Type().Field(i).Tag.Get(tagname[0])
		tags := SQLStructTagBuilder{}.BuildFromString(stringTags)
		if tags.ColumnName != "" {
			m[tags.ColumnName] = name
		} else {
			m[name] = name
		}
	}
	return func(s string) string {
		if r, ok := m[s]; ok {
			return r
		}
		return s
	}, nil
}

// SQLStructTag is the type representation of sql struct tag
type SQLStructTag struct {
	ColumnName       string
	PersistZeroValue bool
}

// SQLStructTagBuilder is a SQLStructTag build
type SQLStructTagBuilder struct {
	logger.Logger
}

func (builder SQLStructTagBuilder) log(args ...interface{}) {
	if builder.Logger != nil {
		builder.Logger.Log(logger.Debug, append([]interface{}{"SQLStructTagBuilder"}, args...)...)
	}
}

// BuildFromString builds a SQLStructTag from a string
func (builder SQLStructTagBuilder) BuildFromString(data string) SQLStructTag {
	datas := strings.Split(data, ",")
	builder.log("datas:", fmt.Sprint(data))
	tag := SQLStructTag{}
	for _, data := range datas {
		data = strings.TrimSpace(data)
		builder.log("\tdata:", fmt.Sprint(data))

		if strings.HasPrefix(data, "column:") {
			tag.ColumnName = strings.TrimSpace(strings.TrimPrefix(data, "column:"))
			builder.log("\t\tcolumn:", fmt.Sprint(tag.ColumnName))

		}
		if strings.TrimSpace(strings.ToLower(data)) == "persistzerovalue" {
			tag.PersistZeroValue = true
		}
	}
	return tag
}
