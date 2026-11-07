package postgres

import (
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Коды ошибок PostgreSQL, на которые опирается логика коннектора.
// Полный перечень: https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	// SQLStateSerializationFailure — 40001, транзакцию нужно перезапустить целиком.
	SQLStateSerializationFailure = "40001"
	// SQLStateDeadlockDetected — 40P01, PostgreSQL сам выбрал жертву дедлока.
	SQLStateDeadlockDetected = "40P01"
	// SQLStateUniqueViolation — 23505, обычно это бизнес-ситуация, а не авария.
	SQLStateUniqueViolation = "23505"
	// SQLStateForeignKeyViolation — 23503.
	SQLStateForeignKeyViolation = "23503"
	// SQLStateCheckViolation — 23514.
	SQLStateCheckViolation = "23514"
	// SQLStateNotNullViolation — 23502.
	SQLStateNotNullViolation = "23502"
	// SQLStateQueryCanceled — 57014, сработал statement_timeout или отмена запроса.
	SQLStateQueryCanceled = "57014"
	// SQLStateLockNotAvailable — 55P03, не удалось взять блокировку за lock_timeout.
	SQLStateLockNotAvailable = "55P03"
	// SQLStateReadOnlySQLTransaction — 25006, запись на реплике.
	SQLStateReadOnlySQLTransaction = "25006"
)

var (
	// ErrNoRows возвращается, когда запрос не вернул ни одной строки.
	// Совпадает с pgx.ErrNoRows, поэтому errors.Is работает с обоими.
	ErrNoRows = pgx.ErrNoRows

	// ErrInvalidConfig — конфигурация не прошла валидацию.
	ErrInvalidConfig = errors.New("postgres: invalid config")

	// ErrClosed — обращение к уже закрытому пулу.
	ErrClosed = errors.New("postgres: pool is closed")

	// ErrNoReplicas — запрошено чтение с реплики, но реплики не сконфигурированы.
	ErrNoReplicas = errors.New("postgres: no replicas configured")

	// ErrPoolExhausted — все соединения заняты. Для readiness-пробы это состояние
	// означает «не готов», хотя база при этом полностью жива.
	ErrPoolExhausted = errors.New("postgres: connection pool is exhausted")

	// ErrReadOnlyInTx — ReadOnly(ctx) внутри транзакции. Транзакция обязана целиком
	// идти на мастер, поэтому молча увести её на реплику нельзя.
	ErrReadOnlyInTx = errors.New("postgres: read-only routing is not allowed inside a transaction")

	// ErrTxClosed — транзакция уже закоммичена или откачена.
	ErrTxClosed = pgx.ErrTxClosed
)

// SQLState возвращает код ошибки PostgreSQL или пустую строку, если ошибка не серверная.
func SQLState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}

	return ""
}

// IsNoRows сообщает, что запрос не вернул строк.
func IsNoRows(err error) bool {
	return errors.Is(err, pgx.ErrNoRows)
}

// IsUniqueViolation — нарушение уникального индекса (23505).
func IsUniqueViolation(err error) bool {
	return SQLState(err) == SQLStateUniqueViolation
}

// IsForeignKeyViolation — нарушение внешнего ключа (23503).
func IsForeignKeyViolation(err error) bool {
	return SQLState(err) == SQLStateForeignKeyViolation
}

// IsCheckViolation — нарушение CHECK-ограничения (23514).
func IsCheckViolation(err error) bool {
	return SQLState(err) == SQLStateCheckViolation
}

// IsNotNullViolation — нарушение NOT NULL (23502).
func IsNotNullViolation(err error) bool {
	return SQLState(err) == SQLStateNotNullViolation
}

// IsSerializationFailure — 40001, транзакция должна быть перезапущена.
func IsSerializationFailure(err error) bool {
	return SQLState(err) == SQLStateSerializationFailure
}

// IsDeadlock — 40P01, PostgreSQL разорвал взаимную блокировку.
func IsDeadlock(err error) bool {
	return SQLState(err) == SQLStateDeadlockDetected
}

// IsQueryCanceled — 57014, сработал statement_timeout либо запрос отменён.
func IsQueryCanceled(err error) bool {
	return SQLState(err) == SQLStateQueryCanceled
}

// IsLockNotAvailable — 55P03, не удалось взять блокировку за lock_timeout.
func IsLockNotAvailable(err error) bool {
	return SQLState(err) == SQLStateLockNotAvailable
}

// IsRetryableTx сообщает, что транзакцию имеет смысл перезапустить целиком.
func IsRetryableTx(err error) bool {
	return IsSerializationFailure(err) || IsDeadlock(err)
}

// IsSafeToRetry сообщает, что запрос гарантированно не дошёл до сервера и его можно
// повторить без риска выполнить операцию дважды.
func IsSafeToRetry(err error) bool {
	return pgconn.SafeToRetry(err)
}

// RetryOn собирает политику повторов по списку кодов SQLSTATE.
// Использовать осознанно: повтор запроса, который мог выполниться, продублирует операцию.
func RetryOn(codes ...string) func(error) bool {
	set := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		set[code] = struct{}{}
	}

	return func(err error) bool {
		state := SQLState(err)
		if state == "" {
			return false
		}

		_, ok := set[state]

		return ok
	}
}
