package masking

import (
	"bytes"
	"sync"
)

const (
	bufDefaultSize = 256
	bufMaxSize     = 4096

	indexPad = 1
)

// bufPool является пулом для переиспользования объектов bytes.Buffer.
// Используется для оптимизации производительности путем уменьшения количества
// выделений памяти при создании новых буферов.
var bufPool = sync.Pool{
	New: func() any {
		b := []byte{bufDefaultSize - indexPad: 0}

		buf := bytes.NewBuffer(b)
		buf.Reset()
		return buf
	},
}

// getNewBuf возвращает новый или переиспользованный буфер из пула bufPool.
func getNewBuf() *bytes.Buffer {
	return bufPool.Get().(*bytes.Buffer) // Ошибка игнорируется по причине: bufPool гарантирует корректность возврата *bytes.Buffer, так как реализация пула полностью контролируется и соответствует ожиданиям, следовательно, дополнительная проверка не требуется
}

// resetAndPutBuffer сбрасывает bytes.Buffer и возвращает его в пул.
// Если буфер слишком велик, он не возвращается в пул для предотвращения
// чрезмерного потребления памяти.
func resetAndPutBuffer(buf *bytes.Buffer) {
	if buf.Cap() > bufMaxSize {
		return
	}

	buf.Reset()
	bufPool.Put(buf)
}
