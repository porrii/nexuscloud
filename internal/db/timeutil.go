package db

import "time"

// Todas las columnas de timestamp se almacenan como TEXT en RFC3339Nano UTC
// en ambos dialectos (ver ADR-003) para evitar diferencias de escaneo de
// tipos entre drivers.

func TimeToString(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func StringToTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// NullableTimeToString devuelve nil (NULL en SQL) para un *time.Time nulo,
// o su representación RFC3339Nano en caso contrario.
func NullableTimeToString(t *time.Time) any {
	if t == nil {
		return nil
	}
	return TimeToString(*t)
}

// ParseNullableTime interpreta un sql.NullString leído de una columna de
// timestamp nullable.
func ParseNullableTime(s string, valid bool) (*time.Time, error) {
	if !valid || s == "" {
		return nil, nil
	}
	t, err := StringToTime(s)
	if err != nil {
		return nil, err
	}
	return &t, nil
}
