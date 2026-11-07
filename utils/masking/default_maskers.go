package masking

import (
	"bytes"
	"unicode"
	"unicode/utf8"
)

const (
	maskSymbol byte = '*'
)

const (
	phoneMinDigitsForUnmasking = 7 // Минимальное количество цифр, при котором начинается частичное скрытие номера.
	phoneUnmaskedDigitsCount   = 4 // Количество цифр, оставляемых открытыми (не замаскированными).
)

const (
	cardMinDigits     = 16 // Минимальное количество цифр для корректного маскирования карты.
	cardUnmaskedLeft  = 2  // Количество цифр слева, оставляемых без маскировки.
	cardUnmaskedRight = 4  // Количество цифр справа, оставляемых без маскировки.
)

const (
	passportTotalDigitsExpected = 10 // Ожидаемое количество цифр в корректном номере паспорта.
	passportUnmaskedLeft        = 4  // Количество цифр слева, оставляемых без маскировки.
	passportUnmaskedRight       = 2  // Количество цифр справа, оставляемых без маскировки.
)

const (
	innMinDigits     = 6 // Минимальное количество цифр для корректного маскирования ИНН.
	innUnmaskedLeft  = 2 // Количество цифр слева, оставляемых без маскировки.
	innUnmaskedRight = 4 // Количество цифр справа, оставляемых без маскировки.
)

const (
	// Если len(local) < 3 → используем фиксированную маску из emailShortLocalMaskWidth символов.
	emailLocalLenPerCharMaskThreshold = 3

	// Длина фиксированной маски для «короткой» локальной части (len(local) < порога).
	// Пример: "ab@example.com" → "*****@example.com".
	emailShortLocalMaskWidth = 5
)

// Перечень встроенных типов маскирования для использования в конфигурации.
// Каждый ключ соответствует функции из набора Mask* ниже.
//
// Phone    — маскирование телефонных номеров;
// Card     — маскирование номеров карт (PAN);
// Passport — маскирование паспортных номеров;
// Fullname — маскирование ФИО (с сохранением пробелов);
// INN      — маскирование ИНН;
// Misc     — «общая» маска символ-к-символу;
// Const    — константная подстановка (используется через правила, без функции).
const (
	Phone    string = "phone"
	Card     string = "card"
	Passport string = "passport"
	Fullname string = "fullname"
	INN      string = "inn"
	Misc     string = "misc"
	Email    string = "email"
	Const    string = "const"
)

var maskFns = map[string]maskFn{
	Phone:    maskPhone,
	Card:     maskCard,
	Passport: maskPassport,
	INN:      maskINN,
	Fullname: maskFullname,
	Misc:     maskMisc,
	Email:    maskEmail,

	Const: nil,
}

type maskFn func([]byte) []byte

// maskPhone маскирует телефонные номера в строке.
// Логика: если общее число цифр < 7 — маскируются все цифры;
// иначе сохраняются последние 4 цифры, остальные цифры заменяются на '*'.
// Разделители/прочие символы сохраняются. Результат — новый слайс.
func maskPhone(s []byte) []byte {
	s = bytes.TrimSpace(s)
	totalDigits := countDigits(s)

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	digitCount := 0

	for _, b := range s {
		if b < '0' || b > '9' {
			buf.WriteByte(b)
			continue
		}

		digitCount++

		if totalDigits < phoneMinDigitsForUnmasking {
			buf.WriteByte(maskSymbol)
			continue
		}

		if digitCount <= totalDigits-phoneUnmaskedDigitsCount {
			buf.WriteByte(maskSymbol)
			continue
		}

		buf.WriteByte(b)
	}

	return bytes.Clone(buf.Bytes())
}

// maskCard маскирует номера карт (PAN) в строке.
// Логика: если цифр < 16 — маскируются все цифры;
// иначе сохраняются первые 2 и последние 4 цифры, остальные заменяются на '*'.
// Пробелы/разделители сохраняются. Результат — новый слайс.
//
//nolint:dupl
func maskCard(s []byte) []byte {
	s = bytes.TrimSpace(s)
	totalDigits := countDigits(s)

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	digitCount := 0

	for _, b := range s {
		if b < '0' || b > '9' {
			buf.WriteByte(b)
			continue
		}

		digitCount++
		if totalDigits < cardMinDigits {
			buf.WriteByte(maskSymbol)
			continue
		}

		if digitCount <= cardUnmaskedLeft || digitCount > totalDigits-cardUnmaskedRight {
			buf.WriteByte(b)
			continue
		}

		buf.WriteByte(maskSymbol)
	}

	return bytes.Clone(buf.Bytes())
}

// maskFullname маскирует ФИО/имена: все непустые руны заменяются на '*',
// пробельные символы сохраняются как есть. Некорректные байты (utf8.RuneError)
// также заменяются на '*'. Результат — новый слайс.
func maskFullname(s []byte) []byte {
	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		if r == utf8.RuneError && size == 1 {
			buf.WriteByte(maskSymbol)
			s = s[1:]
			continue
		}

		if unicode.IsSpace(r) {
			buf.Write(s[:size])
			s = s[size:]
			continue
		}

		buf.WriteByte(maskSymbol)
		s = s[size:]
	}

	return bytes.Clone(buf.Bytes())
}

// maskINN маскирует ИНН: если цифр < 6 — маскируются все цифры;
// иначе сохраняются первые 2 и последние 4 цифры, остальные заменяются на '*'.
// Разделители/прочие символы сохраняются. Результат — новый слайс.
//
//nolint:dupl
func maskINN(s []byte) []byte {
	s = bytes.TrimSpace(s)
	totalDigits := countDigits(s)

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	digitCount := 0

	for _, b := range s {
		if b < '0' || b > '9' {
			buf.WriteByte(b)
			continue
		}

		digitCount++
		if totalDigits < innMinDigits {
			buf.WriteByte(maskSymbol)
			continue
		}

		if digitCount <= innUnmaskedLeft || digitCount > totalDigits-innUnmaskedRight {
			buf.WriteByte(b)
			continue
		}

		buf.WriteByte(maskSymbol)
	}

	return bytes.Clone(buf.Bytes())
}

// maskPassport маскирует паспортные номера: если ровно 10 цифр — сохраняются
// первые 4 и последние 2, остальные цифры заменяются на '*'; иначе маскируются
// все цифры. Разделители/прочие символы сохраняются. Результат — новый слайс.
//
//nolint:dupl
func maskPassport(s []byte) []byte {
	s = bytes.TrimSpace(s)
	totalDigits := countDigits(s)

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	digitCount := 0

	for _, b := range s {
		if b < '0' || b > '9' {
			buf.WriteByte(b)
			continue
		}

		digitCount++
		if totalDigits != passportTotalDigitsExpected {
			buf.WriteByte(maskSymbol)
			continue
		}

		if digitCount <= passportUnmaskedLeft || digitCount > passportTotalDigitsExpected-passportUnmaskedRight {
			buf.WriteByte(b)
			continue
		}
		buf.WriteByte(maskSymbol)
	}

	return bytes.Clone(buf.Bytes())
}

// maskMisc выполняет «общую» маскировку: заменяет каждую руну входа на '*'
// (включая пробелы). Некорректные байты (utf8.RuneError) тоже заменяются на '*'.
// Результат — новый слайс.
func maskMisc(s []byte) []byte {
	s = bytes.TrimSpace(s)

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	for len(s) > 0 {
		r, size := utf8.DecodeRune(s)
		if r == utf8.RuneError && size == 1 {
			buf.WriteByte(maskSymbol)
			s = s[1:]
			continue
		}

		buf.WriteByte(maskSymbol)
		s = s[size:]
	}

	return bytes.Clone(buf.Bytes())
}

func countDigits(b []byte) int {
	var totalDigits int
	for _, b := range b {
		if b >= '0' && b <= '9' {
			totalDigits++
		}
	}

	return totalDigits
}

// maskEmail маскирует e-mail: после TrimSpace заменяет всю локаль до '@' на '*'
// (если локаль < 3 байт — ровно 5 '*'), домен оставляет как есть.
// Если не похоже на addr-spec (нет '@' или он на краях) — вызывает maskMisc.
func maskEmail(s []byte) []byte {
	s = bytes.TrimSpace(s)
	at := bytes.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return maskMisc(s)
	}

	local := s[:at]
	domain := s[at+1:]

	buf := getNewBuf()
	defer resetAndPutBuffer(buf)

	if len(local) < emailLocalLenPerCharMaskThreshold {
		for i := 0; i < emailShortLocalMaskWidth; i++ {
			buf.WriteByte(maskSymbol)
		}
	} else {
		for range local {
			buf.WriteByte(maskSymbol)
		}
	}

	buf.WriteByte('@')
	buf.Write(domain)

	return bytes.Clone(buf.Bytes())
}
