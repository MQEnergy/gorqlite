package gorqlite

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

// NullString represents a string that may be null.
type NullString struct {
	String string
	Valid  bool // Valid is true if String is not NULL
}

// NullInt64 represents an int64 that may be null.
type NullInt64 struct {
	Int64 int64
	Valid bool // Valid is true if Int64 is not NULL
}

// NullInt32 represents an int32 that may be null.
type NullInt32 struct {
	Int32 int32
	Valid bool // Valid is true if Int32 is not NULL
}

// NullInt16 represents an int16 that may be null.
type NullInt16 struct {
	Int16 int16
	Valid bool // Valid is true if Int16 is not NULL
}

// NullFloat64 represents a float64 that may be null.
type NullFloat64 struct {
	Float64 float64
	Valid   bool // Valid is true if Float64 is not NULL
}

// NullBool represents a bool that may be null.
type NullBool struct {
	Bool  bool
	Valid bool // Valid is true if Bool is not NULL
}

// NullTime represents a time.Time that may be null.
type NullTime struct {
	Time  time.Time
	Valid bool // Valid is true if Time is not NULL
}

/* *****************************************************************

   method: Connection.Query()

	This is the JSON we get back:

{
    "results": [
        {
            "columns": [
                "id",
                "name"
            ],
            "types": [
                "integer",
                "text"
            ],
            "values": [
                [
                    1,
                    "fiona"
                ],
                [
                    2,
                    "sinead"
                ]
            ],
            "time": 0.0150043
        }
    ],
    "time": 0.0220043
}

	or

{
    "results": [
        {
            "columns": [
                "id",
                "name"
            ],
            "types": [
                "number",
                "text"
            ],
            "values": [
                [
                    null,
                    "Hulk"
                ]
            ],
            "time": 4.8958e-05
        },
        {
            "columns": [
                "id",
                "name"
            ],
            "types": [
                "number",
                "text"
            ],
            "time": 1.8460000000000003e-05
        }
    ],
    "time": 0.000134776
}

	or

{
    "results": [
        {
            "error": "near \"nonsense\": syntax error"
        }
    ],
    "time": 2.478862
}

 * *****************************************************************/

/*
QueryOne() is a convenience method that wraps Query() into a single-statement method.
*/
func (conn *Connection) QueryOne(sqlStatement string) (qr QueryResult, err error) {
	if conn.hasBeenClosed {
		qr.Err = errClosed
		return qr, errClosed
	}
	sqlStatements := make([]string, 0)
	sqlStatements = append(sqlStatements, sqlStatement)
	qra, err := conn.Query(sqlStatements)
	return qra[0], err
}

/*
QueryOneParameterized() is a convenience method that wraps QueryParameterized() into a single-statement method.
*/
func (conn *Connection) QueryOneParameterized(statement ParameterizedStatement) (qr QueryResult, err error) {
	if conn.hasBeenClosed {
		qr.Err = errClosed
		return qr, errClosed
	}
	qra, err := conn.QueryParameterized([]ParameterizedStatement{statement})
	return qra[0], err
}

/*
Query() is a convenience method that wraps QueryParameterized() into a single-statement method without parameters.
*/
func (conn *Connection) Query(sqlStatements []string) (results []QueryResult, err error) {
	parameterizedStatements := make([]ParameterizedStatement, 0, len(sqlStatements))
	for _, sqlStatement := range sqlStatements {
		parameterizedStatements = append(parameterizedStatements, ParameterizedStatement{
			Query: sqlStatement,
		})
	}
	return conn.QueryParameterized(parameterizedStatements)
}

/*
QueryParameterized() is used to perform SELECT operations in the database.

It takes an array of parameterized SQL statements and executes them in a single transaction, returning an array of QueryResult vars.
*/
func (conn *Connection) QueryParameterized(sqlStatements []ParameterizedStatement) (results []QueryResult, err error) {
	results = make([]QueryResult, 0)

	if conn.hasBeenClosed {
		var errResult QueryResult
		errResult.Err = errClosed
		results = append(results, errResult)
		return results, errClosed
	}
	trace("%s: Query() for %d statements", conn.ID, len(sqlStatements))

	// if we get an error POSTing, that's a showstopper
	response, err := conn.rqliteApiPost(api_QUERY, sqlStatements)
	if err != nil {
		trace("%s: rqliteApiCall() ERROR: %s", conn.ID, err.Error())
		var errResult QueryResult
		errResult.Err = err
		results = append(results, errResult)
		return results, err
	}
	trace("%s: rqliteApiCall() OK", conn.ID)

	// if we get an error Unmarshaling, that's a showstopper
	var sections map[string]interface{}
	err = json.Unmarshal(response, &sections)
	if err != nil {
		trace("%s: json.Unmarshal() ERROR: %s", conn.ID, err.Error())
		var errResult QueryResult
		errResult.Err = err
		results = append(results, errResult)
		return results, err
	}

	/*
		at this point, we have a "results" section and
		a "time" section.  we can ignore the latter.
	*/

	resultsArray := sections["results"].([]interface{})
	trace("%s: I have %d result(s) to parse", conn.ID, len(resultsArray))

	numStatementErrors := 0
	for n, r := range resultsArray {
		trace("%s: parsing result %d", conn.ID, n)
		var thisQR QueryResult
		thisQR.conn = conn

		// r is a hash with columns, types, values, and time
		thisResult := r.(map[string]interface{})

		// did we get an error?
		_, ok := thisResult["error"]
		if ok {
			trace("%s: have an error on this result: %s", conn.ID, thisResult["error"].(string))
			thisQR.Err = errors.New(thisResult["error"].(string))
			results = append(results, thisQR)
			numStatementErrors++
			continue
		}

		// time is a float64 (could be nil)
		_, ok = thisResult["time"]
		if ok {
			thisQR.Timing = thisResult["time"].(float64)
		}

		// column & type are an array of strings
		c := thisResult["columns"].([]interface{})
		t := thisResult["types"].([]interface{})
		for i := 0; i < len(c); i++ {
			thisQR.columns = append(thisQR.columns, c[i].(string))
			thisQR.types = append(thisQR.types, t[i].(string))
		}

		// and values are an array of arrays
		if thisResult["values"] != nil {
			thisQR.values = thisResult["values"].([]interface{})
		} else {
			trace("%s: fyi, no values this query", conn.ID)
		}

		thisQR.rowNumber = -1

		trace("%s: this result (#col,time) %d %f", conn.ID, len(thisQR.columns), thisQR.Timing)
		results = append(results, thisQR)
	}

	trace("%s: finished parsing, returning %d results", conn.ID, len(results))

	if numStatementErrors > 0 {
		return results, errors.New(fmt.Sprintf("there were %d statement errors", numStatementErrors))
	} else {
		return results, nil
	}
}

/* *****************************************************************

   type: QueryResult

 * *****************************************************************/

/*
A QueryResult type holds the results of a call to Query().  You could think of it as a rowset.

So if you were to query:

  SELECT id, name FROM some_table;

then a QueryResult would hold any errors from that query, a list of columns and types, and the actual row values.

Query() returns an array of QueryResult vars, while QueryOne() returns a single variable.
*/
type QueryResult struct {
	conn      *Connection
	Err       error
	columns   []string
	types     []string
	Timing    float64
	values    []interface{}
	rowNumber int64
}

// these are done as getters rather than as public
// variables to prevent monkey business by the user
// that would put us in an inconsistent state

/* *****************************************************************

   method: QueryResult.Columns()

 * *****************************************************************/

/*
Columns returns a list of the column names for this QueryResult.
*/
func (qr *QueryResult) Columns() []string {
	return qr.columns
}

/* *****************************************************************

   method: QueryResult.Map()

 * *****************************************************************/

/*
Map() returns the current row (as advanced by Next()) as a map[string]interface{}

The key is a string corresponding to a column name.
The value is the corresponding column.

Note that only json values are supported, so you will need to type the interface{} accordingly.
*/
func (qr *QueryResult) Map() (map[string]interface{}, error) {
	trace("%s: Map() called for row %d", qr.conn.ID, qr.rowNumber)
	ans := make(map[string]interface{})

	if qr.rowNumber == -1 {
		return ans, errors.New("you need to Next() before you Map(), sorry, it's complicated")
	}

	thisRowValues := qr.values[qr.rowNumber].([]interface{})
	for i := 0; i < len(qr.columns); i++ {
		switch qr.types[i] {
		case "date", "datetime":
			if thisRowValues[i] != nil {
				t, err := toTime(thisRowValues[i])
				if err != nil {
					return ans, err
				}
				ans[qr.columns[i]] = t
			} else {
				ans[qr.columns[i]] = nil
			}
		default:
			ans[qr.columns[i]] = thisRowValues[i]
		}
	}

	return ans, nil
}

/* *****************************************************************

	method: QueryResult.Next()

 * *****************************************************************/

/*
Next() positions the QueryResult result pointer so that Scan() or Map() is ready.

You should call Next() first, but gorqlite will fix it if you call Map() or Scan() before
the initial Next().

A common idiom:

	rows := conn.Write(something)
	for rows.Next() {
		// your Scan/Map and processing here.
	}
*/
func (qr *QueryResult) Next() bool {
	if qr.rowNumber >= int64(len(qr.values)-1) {
		return false
	}

	qr.rowNumber += 1
	return true
}

/* *****************************************************************

   method: QueryResult.NumRows()

 * *****************************************************************/

/*
NumRows() returns the number of rows returned by the query.
*/
func (qr *QueryResult) NumRows() int64 {
	return int64(len(qr.values))
}

/* *****************************************************************

   method: QueryResult.RowNumber()

 * *****************************************************************/

/*
RowNumber() returns the current row number as Next() iterates through the result's rows.
*/
func (qr *QueryResult) RowNumber() int64 {
	return qr.rowNumber
}

func toTime(src interface{}) (time.Time, error) {
	switch src := src.(type) {
	case string:
		const layout = "2006-01-02 15:04:05"
		if t, err := time.Parse(layout, src); err == nil {
			return t, nil
		}
		return time.Parse(time.RFC3339, src)
	case float64:
		return time.Unix(int64(src), 0), nil
	case int64:
		return time.Unix(src, 0), nil
	}
	return time.Time{}, fmt.Errorf("invalid time type:%T val:%v", src, src)
}

/* *****************************************************************

   method: QueryResult.Scan()

 * *****************************************************************/

/*
Scan() takes a list of pointers and then updates them to reflect he current row's data.

Note that only the following data types are used, and they
are a subset of the types JSON uses:
	string, for JSON strings
	float64, for JSON numbers
	int64, as a convenient extension
	nil for JSON null

booleans, JSON arrays, and JSON objects are not supported,
since sqlite does not support them.
*/
func (qr *QueryResult) Scan(dest ...interface{}) error {
	trace("%s: Scan() called for %d vars", qr.conn.ID, len(dest))

	if qr.rowNumber == -1 {
		return errors.New("you need to Next() before you Scan(), sorry, it's complicated")
	}

	if len(dest) != len(qr.columns) {
		return fmt.Errorf("expected %d columns but got %d vars", len(qr.columns), len(dest))
	}

	thisRowValues := qr.values[qr.rowNumber].([]interface{})
	for n, d := range dest {
		src := thisRowValues[n]
		switch d := d.(type) {
		case *time.Time:
			if src == nil {
				continue
			}
			t, err := toTime(src)
			if err != nil {
				return fmt.Errorf("%v: bad time col:(%d/%s) val:%v", err, n, qr.Columns()[n], src)
			}
			*d = t
		case *int:
			switch src := src.(type) {
			case float64:
				*d = int(src)
			case int64:
				*d = int(src)
			case string:
				i, err := strconv.Atoi(src)
				if err != nil {
					return err
				}
				*d = i
			case nil:
				trace("%s: skipping nil scan data for variable #%d (%s)", qr.conn.ID, n, qr.columns[n])
			default:
				return fmt.Errorf("invalid int col:%d type:%T val:%v", n, src, src)
			}
		case *int64:
			switch src := src.(type) {
			case float64:
				*d = int64(src)
			case int64:
				*d = src
			case string:
				i, err := strconv.ParseInt(src, 10, 64)
				if err != nil {
					return err
				}
				*d = i
			case nil:
				trace("%s: skipping nil scan data for variable #%d (%s)", qr.conn.ID, n, qr.columns[n])
			default:
				return fmt.Errorf("invalid int64 col:%d type:%T val:%v", n, src, src)
			}
		case *float64:
			switch src := src.(type) {
			case float64:
				*d = src
			case int64:
				*d = float64(src)
			case string:
				f, err := strconv.ParseFloat(src, 64)
				if err != nil {
					return err
				}
				*d = f
			case nil:
				trace("%s: skipping nil scan data for variable #%d (%s)", qr.conn.ID, n, qr.columns[n])
			default:
				return fmt.Errorf("invalid float64 col:%d type:%T val:%v", n, src, src)
			}
		case *string:
			switch src := src.(type) {
			case string:
				*d = src
			case nil:
				trace("%s: skipping nil scan data for variable #%d (%s)", qr.conn.ID, n, qr.columns[n])
			default:
				return fmt.Errorf("invalid string col:%d type:%T val:%v", n, src, src)
			}
		case *bool:
			switch src := src.(type) {
			case float64:
				b, err := strconv.ParseBool(strconv.FormatFloat(src, 'g', -1, 64))
				if err != nil {
					return err
				}
				*d = b
			case int64:
				b, err := strconv.ParseBool(strconv.FormatInt(src, 10))
				if err != nil {
					return err
				}
				*d = b
			case string:
				b, err := strconv.ParseBool(src)
				if err != nil {
					return err
				}
				*d = b
			case nil:
				trace("%s: skipping nil scan data for variable #%d (%s)", qr.conn.ID, n, qr.columns[n])
			default:
				return fmt.Errorf("invalid bool col:%d type:%T val:%v", n, src, src)
			}
		case *NullString:
			switch src := src.(type) {
			case string:
				*d = NullString{Valid: true, String: src}
			case nil:
				*d = NullString{Valid: false}
			default:
				return fmt.Errorf("invalid string col:%d type:%T val:%v", n, src, src)
			}
		case *NullInt64:
			switch src := src.(type) {
			case float64:
				*d = NullInt64{Valid: true, Int64: int64(src)}
			case int64:
				*d = NullInt64{Valid: true, Int64: src}
			case string:
				i, err := strconv.ParseInt(src, 10, 64)
				if err != nil {
					return err
				}
				*d = NullInt64{Valid: true, Int64: i}
			case nil:
				*d = NullInt64{Valid: false}
			default:
				return fmt.Errorf("invalid int64 col:%d type:%T val:%v", n, src, src)
			}
		case *NullInt32:
			switch src := src.(type) {
			case float64:
				*d = NullInt32{Valid: true, Int32: int32(src)}
			case int64:
				*d = NullInt32{Valid: true, Int32: int32(src)}
			case string:
				i, err := strconv.ParseInt(src, 10, 32)
				if err != nil {
					return err
				}
				*d = NullInt32{Valid: true, Int32: int32(i)}
			case nil:
				*d = NullInt32{Valid: false}
			default:
				return fmt.Errorf("invalid int32 col:%d type:%T val:%v", n, src, src)
			}
		case *NullInt16:
			switch src := src.(type) {
			case float64:
				*d = NullInt16{Valid: true, Int16: int16(src)}
			case int64:
				*d = NullInt16{Valid: true, Int16: int16(src)}
			case string:
				i, err := strconv.ParseInt(src, 10, 16)
				if err != nil {
					return err
				}
				*d = NullInt16{Valid: true, Int16: int16(i)}
			case nil:
				*d = NullInt16{Valid: false}
			default:
				return fmt.Errorf("invalid int16 col:%d type:%T val:%v", n, src, src)
			}
		case *NullFloat64:
			switch src := src.(type) {
			case float64:
				*d = NullFloat64{Valid: true, Float64: src}
			case int64:
				*d = NullFloat64{Valid: true, Float64: float64(src)}
			case string:
				f, err := strconv.ParseFloat(src, 64)
				if err != nil {
					return err
				}
				*d = NullFloat64{Valid: true, Float64: f}
			case nil:
				*d = NullFloat64{Valid: false}
			default:
				return fmt.Errorf("invalid float64 col:%d type:%T val:%v", n, src, src)
			}
		case *NullBool:
			switch src := src.(type) {
			case float64:
				b, err := strconv.ParseBool(strconv.FormatFloat(src, 'g', -1, 64))
				if err != nil {
					return err
				}
				*d = NullBool{Valid: true, Bool: b}
			case int64:
				b, err := strconv.ParseBool(strconv.FormatInt(src, 10))
				if err != nil {
					return err
				}
				*d = NullBool{Valid: true, Bool: b}
			case string:
				b, err := strconv.ParseBool(src)
				if err != nil {
					return err
				}
				*d = NullBool{Valid: true, Bool: b}
			case nil:
				*d = NullBool{Valid: false}
			default:
				return fmt.Errorf("invalid bool col:%d type:%T val:%v", n, src, src)
			}
		case *NullTime:
			if src == nil {
				*d = NullTime{Valid: false}
			} else {
				t, err := toTime(src)
				if err != nil {
					return fmt.Errorf("%v: bad time col:(%d/%s) val:%v", err, n, qr.Columns()[n], src)
				}
				*d = NullTime{Valid: true, Time: t}
			}
		default:
			return fmt.Errorf("unknown destination type (%T) to scan into in variable #%d", d, n)
		}
	}

	return nil
}

/* *****************************************************************

   method: QueryResult.Types()

 * *****************************************************************/

/*
Types() returns an array of the column's types.

Note that sqlite will repeat the type you tell it, but in many cases, it's ignored.  So you can initialize a column as CHAR(3) but it's really TEXT.  See https://www.sqlite.org/datatype3.html

This info may additionally conflict with the reality that your data is being JSON encoded/decoded.
*/
func (qr *QueryResult) Types() []string {
	return qr.types
}
