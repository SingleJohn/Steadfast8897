package handlers

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

func customSqlReport(c *gin.Context, state *AppState) {
	q := strings.TrimSpace(c.Query("Query"))
	if q == "" {
		q = `SELECT * FROM "PlaybackActivity" ORDER BY "DateCreated" DESC LIMIT 500`
	}
	if !isSafePlaybackActivityQuery(q) {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Query not allowed"})
		return
	}

	rows, err := state.DB.Query(c.Request.Context(), q)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	data, err := rowsToMaps(rows)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, data)
}

type customQueryBody struct {
	CustomQueryString string `json:"CustomQueryString"`
	ReplaceUserId     bool   `json:"ReplaceUserId"`
}

func submitCustomQuery(c *gin.Context, state *AppState) {
	var body customQueryBody
	if err := c.ShouldBindJSON(&body); err != nil || body.CustomQueryString == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "CustomQueryString required"})
		return
	}
	sql := body.CustomQueryString
	trimmed := strings.ToUpper(strings.TrimSpace(sql))
	if !strings.HasPrefix(trimmed, "SELECT") {
		c.JSON(http.StatusForbidden, gin.H{"message": "Only SELECT queries allowed"})
		return
	}
	forbidden := []string{"INSERT", "UPDATE", "DELETE", "DROP", "ALTER", "CREATE", "TRUNCATE",
		"GRANT", "REVOKE", "COPY", "EXECUTE", "DO ", "CALL", "SET ",
		"PG_READ_FILE", "PG_WRITE_FILE", "PG_SLEEP", "LO_IMPORT", "LO_EXPORT"}
	for _, kw := range forbidden {
		// Use word-boundary matching to avoid false positives (e.g. DateCreated containing CREATE)
		kwTrimmed := strings.TrimSpace(kw)
		pattern := `(?i)\b` + regexp.QuoteMeta(kwTrimmed) + `\b`
		if matched, _ := regexp.MatchString(pattern, trimmed); matched {
			c.JSON(http.StatusForbidden, gin.H{"message": "Forbidden keyword: " + kw})
			return
		}
	}
	allowedTables := []string{"PlaybackActivity", "playback_activity", "items", "users",
		"media_versions", "media_streams", "user_item_data", "genres", "item_genres"}
	hasAllowed := false
	sqlLower := strings.ToLower(sql)
	for _, t := range allowedTables {
		if strings.Contains(sqlLower, strings.ToLower(t)) {
			hasAllowed = true
			break
		}
	}
	if !hasAllowed {
		c.JSON(http.StatusForbidden, gin.H{"message": "Query must reference a known table"})
		return
	}

	// Step 1: SQLite function rewrites (before column name mapping)
	sql = rewriteSubstrInstr.ReplaceAllString(sql, "split_part($1, '$2', 1)")
	sql = rewriteInstr.ReplaceAllString(sql, "POSITION($2 IN $1)")
	sql = rewriteStrftimeYMD.ReplaceAllString(sql, "TO_CHAR($1, 'YYYY-MM-DD')")
	sql = rewriteStrftimeH.ReplaceAllString(sql, "TO_CHAR($1, 'HH24')")
	sql = rewriteStrftimeW.ReplaceAllString(sql, "EXTRACT(DOW FROM $1)::text")
	sql = rewriteDatetimeDays.ReplaceAllString(sql, "(NOW() - INTERVAL '$1 days')")
	sql = rewriteDatetimeNow.ReplaceAllString(sql, "NOW()")
	sql = rewriteRowID.ReplaceAllString(sql, "id")
	sql = rewriteUserList.ReplaceAllString(sql, "SELECT id::text FROM users WHERE is_admin = true")

	// Step 2: Fix GROUP BY before column quoting (aliases like "name" are unquoted)
	sql = fixLooseGroupBy(sql)

	// Step 3: Table + column name mapping for PG case sensitivity
	sql = strings.ReplaceAll(sql, "PlaybackActivity", `"PlaybackActivity"`)
	embyColumns := map[string]string{
		"UserId": `"UserId"`, "DateCreated": `"DateCreated"`, "ItemId": `"ItemId"`,
		"ItemType": `"ItemType"`, "ItemName": `"ItemName"`, "PlayDuration": `"PlayDuration"`,
		"PauseDuration": `"PauseDuration"`, "ClientName": `"ClientName"`, "DeviceName": `"DeviceName"`,
		"RemoteAddress": `"RemoteAddress"`, "ClientIp": `"ClientIp"`, "PlaybackMethod": `"PlaybackMethod"`,
		"SeriesName": `"SeriesName"`,
	}
	for embyCol, pgCol := range embyColumns {
		re := regexp.MustCompile(`\b` + embyCol + `\b`)
		sql = re.ReplaceAllString(sql, pgCol)
	}
	rows, err := state.DB.Query(c.Request.Context(), sql)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var results [][]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		results = append(results, vals)
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
		return
	}
	if results == nil {
		results = [][]interface{}{}
	}
	if body.ReplaceUserId {
		replaceUserIDsInResults(c.Request.Context(), state, columns, results)
	}
	c.JSON(http.StatusOK, gin.H{"colums": columns, "columns": columns, "results": results})
}

func replaceUserIDsInResults(ctx context.Context, state *AppState, columns []string, results [][]interface{}) {
	userIDCols := make([]int, 0, 1)
	for i, col := range columns {
		if strings.EqualFold(col, "UserId") {
			userIDCols = append(userIDCols, i)
		}
	}
	if len(userIDCols) == 0 {
		return
	}

	names := map[string]string{}
	for _, row := range results {
		for _, idx := range userIDCols {
			if idx < 0 || idx >= len(row) {
				continue
			}
			if row[idx] == nil {
				continue
			}
			userID := strings.TrimSpace(fmt.Sprint(row[idx]))
			if userID == "" {
				continue
			}
			name, ok := names[userID]
			if !ok {
				name = userID
				_ = state.DB.QueryRow(ctx, "SELECT name FROM users WHERE id = $1::uuid", userID).Scan(&name)
				names[userID] = name
			}
			row[idx] = name
		}
	}
}

var (
	rewriteRowID        = regexp.MustCompile(`(?i)\browid\b`)
	rewriteStrftimeYMD  = regexp.MustCompile(`(?i)strftime\s*\(\s*'%Y-%m-%d'\s*,\s*(\w+)\s*\)`)
	rewriteStrftimeH    = regexp.MustCompile(`(?i)strftime\s*\(\s*'%H'\s*,\s*(\w+)\s*\)`)
	rewriteStrftimeW    = regexp.MustCompile(`(?i)strftime\s*\(\s*'%w'\s*,\s*(\w+)\s*\)`)
	rewriteDatetimeDays = regexp.MustCompile(`(?i)datetime\s*\(\s*'now'\s*,\s*'-(\d+)\s+days?'\s*\)`)
	rewriteDatetimeNow  = regexp.MustCompile(`(?i)datetime\s*\(\s*'now'\s*\)`)
	rewriteSubstrInstr  = regexp.MustCompile(`(?i)substr\s*\(\s*(\w+)\s*,\s*0\s*,\s*instr\s*\(\s*\w+\s*,\s*'([^']+)'\s*\)\s*\)`)
	rewriteInstr        = regexp.MustCompile(`(?i)instr\s*\(\s*(\w+)\s*,\s*'([^']+)'\s*\)`)
	rewriteUserList     = regexp.MustCompile(`(?i)select\s+UserId\s+from\s+UserList`)
)

// fixLooseGroupBy detects SELECT columns not in GROUP BY and not in aggregate functions,
// then wraps them with MIN() to satisfy PostgreSQL strict GROUP BY rules.
func fixLooseGroupBy(sql string) string {
	upper := strings.ToUpper(sql)
	groupByIdx := strings.LastIndex(upper, "GROUP BY")
	if groupByIdx < 0 {
		return sql
	}

	// Extract GROUP BY columns
	afterGroupBy := sql[groupByIdx+8:]
	// Cut at ORDER BY / LIMIT / HAVING if present
	for _, kw := range []string{"ORDER BY", "LIMIT", "HAVING"} {
		if idx := strings.Index(strings.ToUpper(afterGroupBy), kw); idx >= 0 {
			afterGroupBy = afterGroupBy[:idx]
		}
	}
	groupCols := make(map[string]bool)
	for _, col := range strings.Split(afterGroupBy, ",") {
		col = strings.TrimSpace(col)
		col = strings.Trim(col, `"`)
		if col != "" {
			groupCols[strings.ToLower(col)] = true
		}
	}

	// Extract SELECT columns (between SELECT and FROM)
	selectIdx := strings.Index(upper, "SELECT")
	fromIdx := strings.Index(upper, "FROM")
	if selectIdx < 0 || fromIdx < 0 || fromIdx <= selectIdx+6 {
		return sql
	}
	selectPart := sql[selectIdx+6 : fromIdx]

	// Parse SELECT columns, wrap non-grouped non-aggregate ones
	var newCols []string
	for _, col := range splitSelectColumns(selectPart) {
		trimmed := strings.TrimSpace(col)
		if trimmed == "" {
			continue
		}

		upperCol := strings.ToUpper(trimmed)
		// Skip if already an aggregate
		isAgg := false
		for _, fn := range []string{"SUM(", "COUNT(", "MAX(", "MIN(", "AVG("} {
			if strings.Contains(upperCol, fn) {
				isAgg = true
				break
			}
		}
		if isAgg {
			newCols = append(newCols, trimmed)
			continue
		}

		// Extract the bare column name and alias (handle "X AS alias")
		bareName := trimmed
		alias := ""
		aliasName := ""
		if asIdx := strings.LastIndex(strings.ToUpper(trimmed), " AS "); asIdx >= 0 {
			bareName = strings.TrimSpace(trimmed[:asIdx])
			alias = strings.TrimSpace(trimmed[asIdx:])
			aliasName = strings.TrimSpace(trimmed[asIdx+4:])
		}
		bareNameClean := strings.ToLower(strings.Trim(bareName, `"`))

		// Check if bare name or alias is in GROUP BY
		if groupCols[bareNameClean] || (aliasName != "" && groupCols[strings.ToLower(aliasName)]) {
			newCols = append(newCols, trimmed)
		} else {
			// Wrap with MIN()
			newCols = append(newCols, "MIN("+bareName+")"+alias)
		}
	}

	return sql[:selectIdx+6] + " " + strings.Join(newCols, ", ") + " " + sql[fromIdx:]
}

// splitSelectColumns splits SELECT column list respecting parentheses.
func splitSelectColumns(s string) []string {
	var result []string
	depth := 0
	start := 0
	for i, c := range s {
		switch c {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				result = append(result, s[start:i])
				start = i + 1
			}
		}
	}
	result = append(result, s[start:])
	return result
}

func isSafePlaybackActivityQuery(q string) bool {
	low := strings.ToLower(strings.TrimSpace(q))
	if !strings.HasPrefix(low, "select") {
		return false
	}
	if strings.Contains(low, ";") {
		return false
	}
	banned := []string{
		"insert ", "update ", "delete ", "drop ", "truncate ", "alter ", "create ",
		"grant ", "revoke ", "pg_", "information_schema", "into ", "copy ",
	}
	for _, b := range banned {
		if strings.Contains(low, b) {
			return false
		}
	}
	return strings.Contains(low, "playbackactivity")
}

func rowsToMaps(rows pgx.Rows) ([]map[string]interface{}, error) {
	fds := rows.FieldDescriptions()
	var out []map[string]interface{}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return nil, err
		}
		m := make(map[string]interface{}, len(fds))
		for i, fd := range fds {
			m[string(fd.Name)] = vals[i]
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
