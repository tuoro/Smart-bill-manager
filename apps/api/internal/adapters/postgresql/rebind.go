package postgresqladapter

import (
	"context"
	"database/sql/driver"
	"strconv"
	"strings"
)

type rebindingConnector struct {
	inner driver.Connector
}

func (c rebindingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	connection, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return rebindingConnection{inner: connection}, nil
}

func (c rebindingConnector) Driver() driver.Driver {
	return c.inner.Driver()
}

type rebindingConnection struct {
	inner driver.Conn
}

func (c rebindingConnection) Prepare(query string) (driver.Stmt, error) {
	return c.inner.Prepare(rebind(query))
}

func (c rebindingConnection) Close() error {
	return c.inner.Close()
}

func (c rebindingConnection) Begin() (driver.Tx, error) {
	return c.inner.Begin()
}

func (c rebindingConnection) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	connection, ok := c.inner.(driver.ConnPrepareContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return connection.PrepareContext(ctx, rebind(query))
}

func (c rebindingConnection) BeginTx(ctx context.Context, options driver.TxOptions) (driver.Tx, error) {
	connection, ok := c.inner.(driver.ConnBeginTx)
	if !ok {
		return nil, driver.ErrSkip
	}
	return connection.BeginTx(ctx, options)
}

func (c rebindingConnection) ExecContext(
	ctx context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Result, error) {
	connection, ok := c.inner.(driver.ExecerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return connection.ExecContext(ctx, rebind(query), arguments)
}

func (c rebindingConnection) QueryContext(
	ctx context.Context,
	query string,
	arguments []driver.NamedValue,
) (driver.Rows, error) {
	connection, ok := c.inner.(driver.QueryerContext)
	if !ok {
		return nil, driver.ErrSkip
	}
	return connection.QueryContext(ctx, rebind(query), arguments)
}

func (c rebindingConnection) Ping(ctx context.Context) error {
	connection, ok := c.inner.(driver.Pinger)
	if !ok {
		return driver.ErrSkip
	}
	return connection.Ping(ctx)
}

func (c rebindingConnection) ResetSession(ctx context.Context) error {
	connection, ok := c.inner.(driver.SessionResetter)
	if !ok {
		return nil
	}
	return connection.ResetSession(ctx)
}

func (c rebindingConnection) IsValid() bool {
	connection, ok := c.inner.(driver.Validator)
	return !ok || connection.IsValid()
}

func (c rebindingConnection) CheckNamedValue(value *driver.NamedValue) error {
	connection, ok := c.inner.(driver.NamedValueChecker)
	if !ok {
		return driver.ErrSkip
	}
	return connection.CheckNamedValue(value)
}

func rebind(query string) string {
	if !strings.Contains(query, "?") {
		return query
	}
	var builder strings.Builder
	builder.Grow(len(query) + 8)
	parameter := 0
	for index := 0; index < len(query); {
		switch {
		case query[index] == '\'':
			next := copyQuoted(&builder, query, index, '\'')
			index = next
		case query[index] == '"':
			next := copyQuoted(&builder, query, index, '"')
			index = next
		case strings.HasPrefix(query[index:], "--"):
			next := strings.IndexByte(query[index:], '\n')
			if next < 0 {
				builder.WriteString(query[index:])
				index = len(query)
				continue
			}
			next += index + 1
			builder.WriteString(query[index:next])
			index = next
		case strings.HasPrefix(query[index:], "/*"):
			next := strings.Index(query[index+2:], "*/")
			if next < 0 {
				builder.WriteString(query[index:])
				index = len(query)
				continue
			}
			next += index + 4
			builder.WriteString(query[index:next])
			index = next
		case query[index] == '$':
			tag, ok := dollarQuoteTag(query[index:])
			if !ok {
				builder.WriteByte(query[index])
				index += 1
				continue
			}
			end := strings.Index(query[index+len(tag):], tag)
			if end < 0 {
				builder.WriteString(query[index:])
				index = len(query)
				continue
			}
			end += index + 2*len(tag)
			builder.WriteString(query[index:end])
			index = end
		case query[index] == '?':
			parameter += 1
			builder.WriteByte('$')
			builder.WriteString(strconv.Itoa(parameter))
			index += 1
		default:
			builder.WriteByte(query[index])
			index += 1
		}
	}
	return builder.String()
}

// RebindQuery 供使用原生 pgx 批处理的本地离线工具复用唯一占位符规则。
func RebindQuery(query string) string {
	return rebind(query)
}

func copyQuoted(builder *strings.Builder, query string, start int, quote byte) int {
	builder.WriteByte(quote)
	for index := start + 1; index < len(query); index += 1 {
		builder.WriteByte(query[index])
		if query[index] != quote {
			continue
		}
		if index+1 < len(query) && query[index+1] == quote {
			builder.WriteByte(query[index+1])
			index += 1
			continue
		}
		return index + 1
	}
	return len(query)
}

func dollarQuoteTag(query string) (string, bool) {
	end := strings.IndexByte(query[1:], '$')
	if end < 0 {
		return "", false
	}
	end += 1
	tag := query[:end+1]
	for _, value := range tag[1 : len(tag)-1] {
		if value != '_' && (value < '0' || value > '9') &&
			(value < 'A' || value > 'Z') && (value < 'a' || value > 'z') {
			return "", false
		}
	}
	return tag, true
}
